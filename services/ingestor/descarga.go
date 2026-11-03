package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	tamanoLote     = 1000
	timeoutEscribe = 60 * time.Second
	// /location es el endpoint pesado (~4 muestras/s por piloto): se pide por
	// piloto y por ventanas de 10 min, desde la largada hasta el fin + margen.
	ventanaUbicacion = 10 * time.Minute
	margenFinSesion  = 30 * time.Minute
)

// fuente describe un endpoint de OpenF1 que se guarda como eventos de un tipo.
type fuente struct {
	endpoint   string
	tipo       string
	campoFecha string
}

// DescargarCarrera baja una sesión de OpenF1 y la deja en `eventos_carrera` y
// `carreras`. Es idempotente: los datos nuevos se escriben con un carga_id y
// solo al terminar bien se borran los de la descarga anterior. Si algo falla,
// se borra lo nuevo y la carrera vieja queda intacta.
func DescargarCarrera(ctx context.Context, db *mongo.Database, cli *clienteOpenF1, sessionKey string) (err error) {
	if _, err := strconv.Atoi(sessionKey); err != nil {
		return fmt.Errorf("session_key inválido %q: debe ser numérico", sessionKey)
	}
	if err := verificarSinSimulacionActiva(ctx, db, sessionKey); err != nil {
		return err
	}

	log.Printf("🚀 Descargando la sesión %s de OpenF1", sessionKey)
	sesiones, err := cli.obtener(ctx, "sessions", "session_key="+sessionKey)
	if err != nil {
		return err
	}
	if len(sesiones) == 0 {
		return fmt.Errorf("OpenF1 no tiene la sesión %s", sessionKey)
	}
	sesion := sesiones[0]
	largada, ok := parsearFecha(sesion["date_start"])
	if !ok {
		return errors.New("la sesión no tiene date_start")
	}
	finSesion, ok := parsearFecha(sesion["date_end"])
	if !ok {
		finSesion = largada.Add(3 * time.Hour)
	}
	log.Printf("⏱️ %s, largada %s", nombreCarrera(sesion), largada.Format(time.RFC3339))

	drivers, err := cli.obtener(ctx, "drivers", "session_key="+sessionKey)
	if err != nil {
		return err
	}
	pilotos, numeros := armarPilotos(drivers)
	if len(numeros) == 0 {
		return errors.New("OpenF1 no devolvió pilotos para la sesión")
	}

	g := &guardador{db: db, carreraID: sessionKey, cargaID: uuid.NewString(), largada: largada, conteo: map[string]int{}}
	defer func() {
		if err != nil {
			g.descartar()
		}
	}()

	// Vueltas primero: con ellas se fechan los stints y se saca total_vueltas.
	laps, err := cli.obtener(ctx, "laps", "session_key="+sessionKey)
	if err != nil {
		return err
	}
	vueltas := tiemposPorVuelta(laps, largada)
	if err := g.guardarVueltas(ctx, laps); err != nil {
		return err
	}

	stints, err := cli.obtener(ctx, "stints", "session_key="+sessionKey)
	if err != nil {
		return err
	}
	if err := g.guardarStints(ctx, stints, vueltas); err != nil {
		return err
	}

	// Endpoints livianos: una petición para toda la sesión.
	for _, f := range []fuente{
		{"pit", "pit", "date"},
		{"weather", "clima", "date"},
		{"race_control", "control", "date"},
	} {
		filas, err := cli.obtener(ctx, f.endpoint, "session_key="+sessionKey)
		if err != nil {
			return err
		}
		if err := g.guardar(ctx, f, filas); err != nil {
			return err
		}
	}

	// Por piloto. /position e /intervals traen pocas filas por piloto (un cambio
	// de posición, un intervalo cada ~4 s): una petición por piloto para toda la
	// sesión, así no se pierde la parrilla, que se registra antes de la largada.
	// /location se parte además en ventanas de tiempo.
	tramos := ventanas(largada, finSesion.Add(margenFinSesion), ventanaUbicacion)
	for _, num := range numeros {
		piloto := "driver_number=" + strconv.Itoa(num)
		for _, f := range []fuente{
			{"position", "posicion", "date"},
			{"intervals", "intervalo", "date"},
		} {
			filas, err := cli.obtener(ctx, f.endpoint, "session_key="+sessionKey, piloto)
			if err != nil {
				return err
			}
			if err := g.guardar(ctx, f, filas); err != nil {
				return err
			}
		}
		ubic := fuente{"location", "ubicacion", "date"}
		for _, v := range tramos {
			filas, err := cli.obtener(ctx, ubic.endpoint, "session_key="+sessionKey, piloto,
				"date>="+v.Desde.UTC().Format(formatoFiltroFecha),
				"date<"+v.Hasta.UTC().Format(formatoFiltroFecha))
			if err != nil {
				return err
			}
			if err := g.guardar(ctx, ubic, filas); err != nil {
				return err
			}
		}
		log.Printf("   piloto %d listo (%d ubicaciones acumuladas)", num, g.conteo["ubicacion"])
	}

	if err := g.confirmar(ctx, sesion, largada, pilotos, totalVueltas(laps)); err != nil {
		return err
	}
	log.Printf("🎉 Sesión %s descargada: %v (omitidos sin fecha: %d)", sessionKey, g.conteo, g.omitidos)
	return nil
}

// verificarSinSimulacionActiva evita reemplazar los eventos de una carrera que
// se está reproduciendo o va a reproducirse.
func verificarSinSimulacionActiva(ctx context.Context, db *mongo.Database, carreraID string) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := db.Collection("simulaciones").CountDocuments(c, bson.M{
		"carrera_id": carreraID,
		"estado":     bson.M{"$in": []string{estadoProgramada, estadoEnCurso}},
	})
	if err != nil {
		return fmt.Errorf("consultando simulaciones: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("la carrera %s tiene una simulación programada o en curso; espera a que termine", carreraID)
	}
	return nil
}

// guardador escribe los eventos de una descarga en lotes, marcados con su carga_id.
type guardador struct {
	db        *mongo.Database
	carreraID string
	cargaID   string
	largada   time.Time
	conteo    map[string]int
	omitidos  int
}

func (g *guardador) evento(tipo string, t int64, fila Registro) bson.M {
	return bson.M{
		"carrera_id":   g.carreraID,
		"carga_id":     g.cargaID,
		"tipo":         tipo,
		"t_carrera_ms": t,
		"timestamp":    g.largada.Add(time.Duration(t) * time.Millisecond),
		"datos":        bson.M{"raw": fila},
	}
}

// guardar fecha cada fila con su campo de fecha y salta (contando) las que no tienen.
func (g *guardador) guardar(ctx context.Context, f fuente, filas []Registro) error {
	docs := make([]any, 0, len(filas))
	for _, fila := range filas {
		fecha, ok := parsearFecha(fila[f.campoFecha])
		if !ok {
			g.omitidos++
			continue
		}
		docs = append(docs, g.evento(f.tipo, tCarreraMs(fecha, g.largada), fila))
	}
	return g.insertar(ctx, f.tipo, docs)
}

func (g *guardador) guardarVueltas(ctx context.Context, laps []Registro) error {
	docs := make([]any, 0, len(laps))
	for _, lap := range laps {
		t, ok := tiempoVuelta(lap, g.largada)
		if !ok {
			g.omitidos++
			continue
		}
		docs = append(docs, g.evento("vuelta", t, lap))
	}
	return g.insertar(ctx, "vuelta", docs)
}

func (g *guardador) guardarStints(ctx context.Context, stints []Registro, vueltas map[int]int64) error {
	docs := make([]any, 0, len(stints))
	for _, s := range stints {
		t, ok := tiempoStint(s, vueltas)
		if !ok {
			g.omitidos++
			continue
		}
		docs = append(docs, g.evento("stint", t, s))
	}
	return g.insertar(ctx, "stint", docs)
}

func (g *guardador) insertar(ctx context.Context, tipo string, docs []any) error {
	col := g.db.Collection("eventos_carrera")
	for i := 0; i < len(docs); i += tamanoLote {
		fin := min(i+tamanoLote, len(docs))
		c, cancel := context.WithTimeout(ctx, timeoutEscribe)
		_, err := col.InsertMany(c, docs[i:fin], options.InsertMany().SetOrdered(false))
		cancel()
		if err != nil {
			return fmt.Errorf("guardando eventos %s: %w", tipo, err)
		}
	}
	g.conteo[tipo] += len(docs)
	return nil
}

// confirmar deja la descarga nueva como la vigente: borra los eventos de cargas
// anteriores y reemplaza el documento de `carreras`.
func (g *guardador) confirmar(ctx context.Context, sesion Registro, largada time.Time, pilotos []bson.M, total int) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := g.db.Collection("eventos_carrera").DeleteMany(c, bson.M{
		"carrera_id": g.carreraID,
		"carga_id":   bson.M{"$ne": g.cargaID},
	}); err != nil {
		return fmt.Errorf("borrando la descarga anterior: %w", err)
	}

	anio, _ := entero(sesion["year"])
	carrera := bson.M{
		"carrera_id":    g.carreraID,
		"nombre":        nombreCarrera(sesion),
		"circuito":      texto(sesion["circuit_short_name"]),
		"temporada":     int32(anio),
		"fuente":        "openf1",
		"session_key":   g.carreraID,
		"hora_largada":  largada,
		"total_vueltas": int32(total),
		"pilotos":       pilotos,
		"creada_en":     time.Now().UTC(),
		"estado":        "descargada",
	}
	if _, err := g.db.Collection("carreras").ReplaceOne(c,
		bson.M{"carrera_id": g.carreraID}, carrera, options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("guardando la carrera: %w", err)
	}
	return nil
}

// descartar borra lo que alcanzó a escribir una descarga fallida. Usa un
// contexto propio porque el de la descarga puede estar cancelado.
func (g *guardador) descartar() {
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := g.db.Collection("eventos_carrera").DeleteMany(c, bson.M{"carga_id": g.cargaID})
	if err != nil {
		log.Printf("❌ no se pudo limpiar la descarga fallida %s: %v", g.cargaID, err)
		return
	}
	log.Printf("🧹 descarga fallida: se borraron %d eventos nuevos; la carrera anterior queda igual", res.DeletedCount)
}
