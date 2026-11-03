package dinamicas

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Estados de `simulaciones` (validador de db/mongo/init).
const (
	SimEnCurso    = "en_curso"
	SimFinalizada = "finalizada"
	SimFallida    = "fallida"
)

// Simulacion es lo que el worker necesita de `simulaciones`.
type Simulacion struct {
	ID         string
	CarreraID  string
	Estado     string
	TInicioMs  int64
	TCarreraMs int64 // reloj de carrera ya emitido; math.MinInt64 si aun no late
	IniciadaEn time.Time
}

// Termino dice si la simulacion ya no va a avanzar.
func (s Simulacion) Termino() bool { return s.Estado == SimFinalizada || s.Estado == SimFallida }

// HoraReal es el instante real en que la reproduccion pasa por t:
// iniciada_en + (t - t_inicio_ms), la misma cuenta del reloj del Ingestor.
// Sale igual cada vez que se reprocesa la simulacion, asi que opens_at y
// closes_at no cambian entre reintentos (Pagos los compara).
func (s Simulacion) HoraReal(t int64) time.Time {
	return s.IniciadaEn.Add(time.Duration(t-s.TInicioMs) * time.Millisecond).UTC()
}

// DatosCarrera es lo que el Gestor necesita de `carreras`.
type DatosCarrera struct {
	TotalVueltas int
	Pilotos      map[int]string // numero -> codigo de 3 letras
}

// Fuente es lo que el worker lee de Mongo. Solo lectura (RNF-18).
type Fuente interface {
	// SimulacionActual es la mas reciente en_curso, finalizada o fallida;
	// ok=false si no hay ninguna.
	SimulacionActual(ctx context.Context) (Simulacion, bool, error)
	Carrera(ctx context.Context, carreraID string) (DatosCarrera, error)
	// Eventos devuelve los de TiposUsados con desde < t_carrera_ms <= hasta,
	// ordenados por (t_carrera_ms, _id) y normalizados.
	Eventos(ctx context.Context, carreraID string, desde, hasta int64) ([]Evento, error)
}

// coleccion es la parte de *mongo.Collection que usa el lector; las pruebas
// la reemplazan por una falsa.
type coleccion interface {
	FindOne(ctx context.Context, filtro any, opts ...*options.FindOneOptions) *mongo.SingleResult
	Find(ctx context.Context, filtro any, opts ...*options.FindOptions) (*mongo.Cursor, error)
}

// LectorMongo lee las colecciones que escribe el Ingestor.
type LectorMongo struct {
	simulaciones coleccion
	carreras     coleccion
	eventos      coleccion
}

func NuevoLectorMongo(db *mongo.Database) *LectorMongo {
	return &LectorMongo{
		simulaciones: db.Collection("simulaciones"),
		carreras:     db.Collection("carreras"),
		eventos:      db.Collection("eventos_carrera"),
	}
}

type docSimulacion struct {
	ID         string    `bson:"_id"`
	CarreraID  string    `bson:"carrera_id"`
	Estado     string    `bson:"estado"`
	TInicioMs  int64     `bson:"t_inicio_ms"`
	TCarreraMs *int64    `bson:"t_carrera_ms"`
	IniciadaEn time.Time `bson:"iniciada_en"`
}

func (l *LectorMongo) SimulacionActual(ctx context.Context) (Simulacion, bool, error) {
	var doc docSimulacion
	err := l.simulaciones.FindOne(ctx,
		bson.M{"estado": bson.M{"$in": bson.A{SimEnCurso, SimFinalizada, SimFallida}}},
		options.FindOne().
			SetSort(bson.D{{Key: "iniciada_en", Value: -1}}).
			SetProjection(bson.M{"carrera_id": 1, "estado": 1, "t_inicio_ms": 1, "t_carrera_ms": 1, "iniciada_en": 1}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Simulacion{}, false, nil
	}
	if err != nil {
		return Simulacion{}, false, fmt.Errorf("leyendo la simulacion actual: %w", err)
	}
	s := Simulacion{
		ID: doc.ID, CarreraID: doc.CarreraID, Estado: doc.Estado,
		TInicioMs: doc.TInicioMs, TCarreraMs: math.MinInt64, IniciadaEn: doc.IniciadaEn.UTC(),
	}
	if doc.TCarreraMs != nil {
		s.TCarreraMs = *doc.TCarreraMs
	}
	return s, true, nil
}

type docCarrera struct {
	TotalVueltas int `bson:"total_vueltas"`
	Pilotos      []struct {
		Numero int    `bson:"numero"`
		Codigo string `bson:"codigo"`
	} `bson:"pilotos"`
}

func (l *LectorMongo) Carrera(ctx context.Context, carreraID string) (DatosCarrera, error) {
	var doc docCarrera
	err := l.carreras.FindOne(ctx, bson.M{"carrera_id": carreraID},
		options.FindOne().SetProjection(bson.M{"total_vueltas": 1, "pilotos.numero": 1, "pilotos.codigo": 1}),
	).Decode(&doc)
	if err != nil {
		return DatosCarrera{}, fmt.Errorf("leyendo la carrera %s: %w", carreraID, err)
	}
	d := DatosCarrera{TotalVueltas: doc.TotalVueltas, Pilotos: make(map[int]string, len(doc.Pilotos))}
	for _, p := range doc.Pilotos {
		d.Pilotos[p.Numero] = p.Codigo
	}
	return d, nil
}

// camposRaw son los campos de datos.raw que usa Normalizar (proyeccion minima).
var camposRaw = []string{
	"driver_number", "position", "interval", "gap_to_leader", "lap_number", "lap_duration",
	"lap_start", "tyre_age_at_start", "compound", "category", "flag", "message",
}

type docEvento struct {
	ID    bson.RawValue `bson:"_id"`
	Tipo  string        `bson:"tipo"`
	T     int64         `bson:"t_carrera_ms"`
	Datos struct {
		Raw bson.M `bson:"raw"`
	} `bson:"datos"`
}

func (l *LectorMongo) Eventos(ctx context.Context, carreraID string, desde, hasta int64) ([]Evento, error) {
	tipos := make(bson.A, 0, len(TiposUsados))
	for _, t := range TiposUsados {
		tipos = append(tipos, string(t))
	}
	proyeccion := bson.M{"tipo": 1, "t_carrera_ms": 1}
	for _, c := range camposRaw {
		proyeccion["datos.raw."+c] = 1
	}
	cur, err := l.eventos.Find(ctx,
		bson.M{
			"carrera_id":   carreraID,
			"tipo":         bson.M{"$in": tipos},
			"t_carrera_ms": bson.M{"$gt": desde, "$lte": hasta},
		},
		options.Find().
			SetSort(bson.D{{Key: "t_carrera_ms", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(proyeccion),
	)
	if err != nil {
		return nil, fmt.Errorf("leyendo eventos (%d, %d]: %w", desde, hasta, err)
	}
	defer cur.Close(ctx)
	var evs []Evento
	for cur.Next(ctx) {
		var doc docEvento
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decodificando un evento: %w", err)
		}
		if ev, ok := convertir(doc); ok {
			evs = append(evs, ev)
		}
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("leyendo eventos (%d, %d]: %w", desde, hasta, err)
	}
	return evs, nil
}

// convertir pasa el documento a Evento; se descarta si le falta un campo.
func convertir(doc docEvento) (Evento, bool) {
	return Normalizar(doc.Tipo, doc.T, textoID(doc.ID), doc.Datos.Raw)
}

// textoID: el Ingestor inserta con ObjectID; el hex conserva su orden.
func textoID(v bson.RawValue) string {
	if oid, ok := v.ObjectIDOK(); ok {
		return oid.Hex()
	}
	if s, ok := v.StringValueOK(); ok {
		return s
	}
	if v.Type == 0 {
		return ""
	}
	return v.String()
}
