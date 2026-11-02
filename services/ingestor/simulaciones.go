package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Estados de una simulación (validador de `simulaciones` en db/mongo/init).
const (
	estadoProgramada = "programada"
	estadoEnCurso    = "en_curso"
	estadoFinalizada = "finalizada"
	estadoFallida    = "fallida"
)

// Mismo rango que valida Admin.
const (
	minutosMin = 1
	minutosMax = 120
)

// Errores que viajan en la respuesta a Admin.
const (
	errCarreraNoDescargada = "carrera_no_descargada"
	errYaHaySimulacion     = "ya_hay_simulacion"
	errOrdenInvalida       = "orden_invalida"
	errInterno             = "error_interno"
)

// ordenSimulacion es la orden de iniciar simulación que llega por el Broker.
type ordenSimulacion struct {
	CarreraID string
	Minutos   int
	TInicioMs int64
}

// respuestaSimulacion es lo que el Ingestor responde por reply_to.
type respuestaSimulacion struct {
	OK           bool       `json:"ok"`
	SimulacionID string     `json:"simulacion_id,omitempty"`
	HoraLargada  *time.Time `json:"hora_largada,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// almacenSimulaciones es lo que programarSimulacion necesita de Mongo.
type almacenSimulaciones interface {
	carreraDescargada(ctx context.Context, carreraID string) (bool, error)
	haySimulacionActiva(ctx context.Context) (bool, error)
	crear(ctx context.Context, doc bson.M) error
}

// programarSimulacion registra la simulación con su hora de largada. Solo hay
// una a la vez: se rechaza si ya hay otra programada o en curso (RF-22). Las
// órdenes se procesan de a una en el consumidor, así que no hay carrera entre
// la revisión y la inserción.
func programarSimulacion(ctx context.Context, alm almacenSimulaciones, orden ordenSimulacion, ahora time.Time) respuestaSimulacion {
	if orden.CarreraID == "" || orden.Minutos < minutosMin || orden.Minutos > minutosMax || orden.TInicioMs < 0 {
		return respuestaSimulacion{Error: errOrdenInvalida}
	}
	existe, err := alm.carreraDescargada(ctx, orden.CarreraID)
	if err != nil {
		log.Printf("❌ verificando la carrera %s: %v", orden.CarreraID, err)
		return respuestaSimulacion{Error: errInterno}
	}
	if !existe {
		return respuestaSimulacion{Error: errCarreraNoDescargada}
	}
	activa, err := alm.haySimulacionActiva(ctx)
	if err != nil {
		log.Printf("❌ verificando simulaciones activas: %v", err)
		return respuestaSimulacion{Error: errInterno}
	}
	if activa {
		return respuestaSimulacion{Error: errYaHaySimulacion}
	}

	ahora = ahora.UTC().Truncate(time.Millisecond)
	largada := ahora.Add(time.Duration(orden.Minutos) * time.Minute)
	id := uuid.NewString()
	if err := alm.crear(ctx, bson.M{
		"_id":          id,
		"carrera_id":   orden.CarreraID,
		"estado":       estadoProgramada,
		"hora_largada": largada,
		"t_inicio_ms":  orden.TInicioMs,
		"creada_en":    ahora,
	}); err != nil {
		log.Printf("❌ guardando la simulación: %v", err)
		return respuestaSimulacion{Error: errInterno}
	}
	log.Printf("✅ Simulación %s programada: carrera %s, largada %s", id, orden.CarreraID, largada.Format(time.RFC3339))
	return respuestaSimulacion{OK: true, SimulacionID: id, HoraLargada: &largada}
}

// almacenMongo implementa almacenSimulaciones con un contexto corto por operación.
type almacenMongo struct {
	db *mongo.Database
}

func (a almacenMongo) carreraDescargada(ctx context.Context, carreraID string) (bool, error) {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	err := a.db.Collection("carreras").FindOne(c, bson.M{"carrera_id": carreraID},
		options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (a almacenMongo) haySimulacionActiva(ctx context.Context) (bool, error) {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	n, err := a.db.Collection("simulaciones").CountDocuments(c, bson.M{
		"estado": bson.M{"$in": []string{estadoProgramada, estadoEnCurso}},
	})
	return n > 0, err
}

func (a almacenMongo) crear(ctx context.Context, doc bson.M) error {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	_, err := a.db.Collection("simulaciones").InsertOne(c, doc)
	return err
}

// carreraResumen es una fila de la lista de carreras descargadas para la CLI.
type carreraResumen struct {
	CarreraID    string    `bson:"carrera_id" json:"carrera_id"`
	Nombre       string    `bson:"nombre" json:"nombre"`
	Circuito     string    `bson:"circuito" json:"circuito"`
	Temporada    int32     `bson:"temporada" json:"temporada"`
	TotalVueltas int32     `bson:"total_vueltas" json:"total_vueltas"`
	HoraLargada  time.Time `bson:"hora_largada" json:"hora_largada"`
	Estado       string    `bson:"estado" json:"estado"`
	Pilotos      int       `bson:"pilotos" json:"pilotos"`
}

// listarCarreras devuelve las carreras que ya están en Mongo (HU2-19): el
// admin nunca le pide datos a OpenF1.
func listarCarreras(ctx context.Context, db *mongo.Database) ([]carreraResumen, error) {
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	cur, err := db.Collection("carreras").Aggregate(c, mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{{Key: "hora_largada", Value: -1}}}},
		{{Key: "$project", Value: bson.M{
			"_id": 0, "carrera_id": 1, "nombre": 1, "circuito": 1, "temporada": 1,
			"total_vueltas": 1, "hora_largada": 1, "estado": 1,
			"pilotos": bson.M{"$size": bson.M{"$ifNull": bson.A{"$pilotos", bson.A{}}}},
		}}},
	})
	if err != nil {
		return nil, fmt.Errorf("listando carreras: %w", err)
	}
	carreras := []carreraResumen{}
	if err := cur.All(c, &carreras); err != nil {
		return nil, fmt.Errorf("leyendo carreras: %w", err)
	}
	return carreras, nil
}
