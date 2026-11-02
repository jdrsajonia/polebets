package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	periodoMonitor = time.Second
	periodoTick    = 200 * time.Millisecond
	periodoLatido  = time.Second
	timeoutOp      = 5 * time.Second
	// Si Mongo falla seguido este tiempo, la simulación se da por fallida.
	limiteFallos = 30 * time.Second
)

// simulacion es lo que la reproducción necesita del documento de `simulaciones`.
type simulacion struct {
	ID         string    `bson:"_id"`
	CarreraID  string    `bson:"carrera_id"`
	TInicioMs  int64     `bson:"t_inicio_ms"`
	IniciadaEn time.Time `bson:"iniciada_en"`
}

// reproductor pone a correr las simulaciones programadas cuando llega su hora.
type reproductor struct {
	db *mongo.Database
}

func (r *reproductor) col(nombre string) *mongo.Collection { return r.db.Collection(nombre) }

// marcarHuerfanas deja como fallida cualquier simulación en_curso al arrancar:
// si el Ingestor se reinició, nadie la está reproduciendo (RNF-16).
func (r *reproductor) marcarHuerfanas(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	var huerfanas []simulacion
	cur, err := r.col("simulaciones").Find(c, bson.M{"estado": estadoEnCurso})
	if err != nil {
		return fmt.Errorf("buscando simulaciones en curso: %w", err)
	}
	if err := cur.All(c, &huerfanas); err != nil {
		return fmt.Errorf("leyendo simulaciones en curso: %w", err)
	}
	for _, s := range huerfanas {
		if err := r.marcarFallida(ctx, s, "el Ingestor se reinició durante la reproducción"); err != nil {
			return err
		}
	}
	return nil
}

// monitorear revisa cada segundo si una simulación programada ya debe largar.
// Corre las reproducciones en esta misma goroutine: solo hay una a la vez (RF-22).
func (r *reproductor) monitorear(ctx context.Context) {
	ticker := time.NewTicker(periodoMonitor)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sim, err := r.tomarSiguiente(ctx)
		if err != nil {
			if !errors.Is(err, mongo.ErrNoDocuments) && ctx.Err() == nil {
				log.Printf("❌ revisando simulaciones programadas: %v", err)
			}
			continue
		}
		log.Printf("🏁 Largada de la simulación %s (carrera %s)", sim.ID, sim.CarreraID)
		r.reproducir(ctx, sim)
	}
}

// tomarSiguiente pasa de programada a en_curso en una sola operación, para que
// la misma simulación no pueda arrancar dos veces.
func (r *reproductor) tomarSiguiente(ctx context.Context) (simulacion, error) {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	// Mongo guarda milisegundos: se trunca para que el reloj en memoria y el
	// iniciada_en guardado sean el mismo instante.
	ahora := time.Now().UTC().Truncate(time.Millisecond)
	var sim simulacion
	err := r.col("simulaciones").FindOneAndUpdate(c,
		bson.M{"estado": estadoProgramada, "hora_largada": bson.M{"$lte": ahora}},
		bson.M{"$set": bson.M{"estado": estadoEnCurso, "iniciada_en": ahora, "latido_en": ahora}},
		options.FindOneAndUpdate().
			SetSort(bson.D{{Key: "hora_largada", Value: 1}}).
			SetReturnDocument(options.After),
	).Decode(&sim)
	return sim, err
}

// preparacion son los datos que se cargan una vez antes de largar.
type preparacion struct {
	totalVueltas int32
	vueltas      indiceVueltas
	tUltimo      int64
}

func (r *reproductor) preparar(ctx context.Context, sim simulacion) (preparacion, error) {
	var p preparacion
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()

	var carrera struct {
		TotalVueltas int32 `bson:"total_vueltas"`
	}
	if err := r.col("carreras").FindOne(c, bson.M{"carrera_id": sim.CarreraID}).Decode(&carrera); err != nil {
		return p, fmt.Errorf("leyendo la carrera: %w", err)
	}
	p.totalVueltas = carrera.TotalVueltas

	var ultimo struct {
		T int64 `bson:"t_carrera_ms"`
	}
	err := r.col("eventos_carrera").FindOne(c, bson.M{"carrera_id": sim.CarreraID},
		options.FindOne().SetSort(bson.D{{Key: "t_carrera_ms", Value: -1}}).SetProjection(bson.M{"t_carrera_ms": 1}),
	).Decode(&ultimo)
	if err != nil {
		return p, fmt.Errorf("buscando el último evento: %w", err)
	}
	p.tUltimo = ultimo.T

	cur, err := r.col("eventos_carrera").Find(c,
		bson.M{"carrera_id": sim.CarreraID, "tipo": "vuelta"},
		options.Find().SetProjection(bson.M{"t_carrera_ms": 1, "datos.raw.lap_number": 1}))
	if err != nil {
		return p, fmt.Errorf("leyendo las vueltas: %w", err)
	}
	var filas []struct {
		T     int64 `bson:"t_carrera_ms"`
		Datos struct {
			Raw struct {
				Vuelta int `bson:"lap_number"`
			} `bson:"raw"`
		} `bson:"datos"`
	}
	if err := cur.All(c, &filas); err != nil {
		return p, fmt.Errorf("leyendo las vueltas: %w", err)
	}
	marcas := make([]marcaVuelta, 0, len(filas))
	for _, f := range filas {
		marcas = append(marcas, marcaVuelta{T: f.T, Vuelta: f.Datos.Raw.Vuelta})
	}
	p.vueltas = nuevoIndiceVueltas(marcas)

	// Se limpia lo que dejó una reproducción anterior de la misma carrera, así
	// la secuencia es la misma cada vez (RNF-20). Puede tocar muchos documentos.
	cLimpia, cancelLimpia := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelLimpia()
	if _, err := r.col("eventos_carrera").UpdateMany(cLimpia,
		bson.M{"carrera_id": sim.CarreraID, "emitido_en": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"emitido_en": "", "simulacion_id": ""}},
	); err != nil {
		return p, fmt.Errorf("limpiando la reproducción anterior: %w", err)
	}

	// `reproducciones` la leen los workers del P1 que siguen en el Motor.
	if _, err := r.col("reproducciones").UpdateOne(c,
		bson.M{"_id": sim.CarreraID},
		bson.M{"$set": bson.M{"iniciada_en": sim.IniciadaEn, "latido_en": sim.IniciadaEn, "terminada_en": nil, "factor": 1.0}},
		options.Update().SetUpsert(true),
	); err != nil {
		return p, fmt.Errorf("registrando la reproducción: %w", err)
	}
	return p, nil
}

// reproducir emite los eventos según el tiempo de carrera hasta el último dato.
func (r *reproductor) reproducir(ctx context.Context, sim simulacion) {
	prep, err := r.preparar(ctx, sim)
	if err != nil {
		r.terminarConFallo(sim, err.Error())
		return
	}

	reloj := nuevoRelojReproduccion(sim.TInicioMs, sim.IniciadaEn)
	ticker := time.NewTicker(periodoTick)
	defer ticker.Stop()
	var ultimoLatido time.Time
	var fallandoDesde time.Time

	for {
		select {
		case <-ctx.Done():
			r.terminarConFallo(sim, "el Ingestor se detuvo durante la reproducción")
			return
		case <-ticker.C:
		}
		ahora := time.Now().UTC()

		err := r.emitir(ctx, sim, reloj, ahora)
		terminada := err == nil && reloj.tAnterior >= prep.tUltimo
		if err == nil && (terminada || ahora.Sub(ultimoLatido) >= periodoLatido) {
			err = r.latir(ctx, sim, reloj.tAnterior, prep, ahora)
			if err == nil {
				ultimoLatido = ahora
			}
		}

		if err != nil {
			if fallandoDesde.IsZero() {
				fallandoDesde = ahora
			}
			log.Printf("❌ simulación %s: %v", sim.ID, err)
			if ahora.Sub(fallandoDesde) >= limiteFallos {
				r.terminarConFallo(sim, "Mongo falló por más de "+limiteFallos.String()+": "+err.Error())
				return
			}
			continue
		}
		fallandoDesde = time.Time{}

		if terminada {
			if err := r.finalizar(ctx, sim, ahora); err != nil {
				log.Printf("❌ finalizando la simulación %s: %v", sim.ID, err)
				continue
			}
			log.Printf("✅ Simulación %s finalizada (t = %d ms)", sim.ID, reloj.tAnterior)
			return
		}
	}
}

// emitir marca con emitido_en los eventos cuyo tiempo de carrera ya llegó.
// Se actualizan en su lugar (sin copiar documentos): el Motor lee por emitido_en.
func (r *reproductor) emitir(ctx context.Context, sim simulacion, reloj *relojReproduccion, ahora time.Time) error {
	rg, ok := reloj.tick(ahora)
	if !ok {
		return nil
	}
	filtroT := bson.M{"$lte": rg.Hasta}
	if !rg.SinDesde {
		filtroT["$gt"] = rg.Desde
	}
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	if _, err := r.col("eventos_carrera").UpdateMany(c,
		bson.M{"carrera_id": sim.CarreraID, "t_carrera_ms": filtroT},
		bson.M{"$set": bson.M{"emitido_en": ahora, "simulacion_id": sim.ID}},
	); err != nil {
		return fmt.Errorf("emitiendo eventos (%d, %d]: %w", rg.Desde, rg.Hasta, err)
	}
	reloj.confirmar(rg)
	return nil
}

// latir publica el avance en la simulación y en `reproducciones`.
func (r *reproductor) latir(ctx context.Context, sim simulacion, t int64, prep preparacion, ahora time.Time) error {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	if _, err := r.col("simulaciones").UpdateOne(c, bson.M{"_id": sim.ID}, bson.M{"$set": bson.M{
		"t_carrera_ms":  t,
		"vuelta_actual": int32(prep.vueltas.vueltaActual(t)),
		"total_vueltas": prep.totalVueltas,
		"latido_en":     ahora,
	}}); err != nil {
		return fmt.Errorf("actualizando el latido: %w", err)
	}
	if _, err := r.col("reproducciones").UpdateOne(c,
		bson.M{"_id": sim.CarreraID}, bson.M{"$set": bson.M{"latido_en": ahora}}); err != nil {
		return fmt.Errorf("actualizando reproducciones: %w", err)
	}
	return nil
}

func (r *reproductor) finalizar(ctx context.Context, sim simulacion, ahora time.Time) error {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	if _, err := r.col("simulaciones").UpdateOne(c, bson.M{"_id": sim.ID},
		bson.M{"$set": bson.M{"estado": estadoFinalizada, "terminada_en": ahora}}); err != nil {
		return err
	}
	_, err := r.col("reproducciones").UpdateOne(c, bson.M{"_id": sim.CarreraID},
		bson.M{"$set": bson.M{"terminada_en": ahora}})
	return err
}

// terminarConFallo usa un contexto propio: el del servicio puede estar cancelado.
func (r *reproductor) terminarConFallo(sim simulacion, motivo string) {
	if err := r.marcarFallida(context.Background(), sim, motivo); err != nil {
		log.Printf("❌ no se pudo marcar fallida la simulación %s: %v", sim.ID, err)
	}
}

func (r *reproductor) marcarFallida(ctx context.Context, sim simulacion, motivo string) error {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	ahora := time.Now().UTC()
	if _, err := r.col("simulaciones").UpdateOne(c,
		bson.M{"_id": sim.ID, "estado": estadoEnCurso},
		bson.M{"$set": bson.M{"estado": estadoFallida, "terminada_en": ahora, "motivo_fallo": motivo}},
	); err != nil {
		return fmt.Errorf("marcando fallida la simulación %s: %w", sim.ID, err)
	}
	if _, err := r.col("reproducciones").UpdateOne(c, bson.M{"_id": sim.CarreraID},
		bson.M{"$set": bson.M{"terminada_en": ahora}}); err != nil {
		return fmt.Errorf("cerrando reproducciones de %s: %w", sim.CarreraID, err)
	}
	log.Printf("⚠️ Simulación %s marcada fallida: %s", sim.ID, motivo)
	return nil
}
