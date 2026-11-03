package carrera

import (
	"context"
	"errors"
	"math"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoFuente lee las colecciones que escribe el Ingestor. Solo lectura (RNF-18).
type MongoFuente struct {
	simulaciones *mongo.Collection
	carreras     *mongo.Collection
	eventos      *mongo.Collection
}

func NuevaMongoFuente(db *mongo.Database) *MongoFuente {
	return &MongoFuente{
		simulaciones: db.Collection("simulaciones"),
		carreras:     db.Collection("carreras"),
		eventos:      db.Collection("eventos_carrera"),
	}
}

func (m *MongoFuente) SimulacionActual(ctx context.Context) (Simulacion, bool, error) {
	var doc struct {
		ID         string `bson:"_id"`
		CarreraID  string `bson:"carrera_id"`
		Estado     string `bson:"estado"`
		TCarreraMs *int64 `bson:"t_carrera_ms"`
	}
	err := m.simulaciones.FindOne(ctx,
		bson.M{"estado": bson.M{"$in": bson.A{"en_curso", "finalizada"}}},
		options.FindOne().SetSort(bson.D{{Key: "iniciada_en", Value: -1}}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Simulacion{}, false, nil
	}
	if err != nil {
		return Simulacion{}, false, err
	}
	s := Simulacion{ID: doc.ID, CarreraID: doc.CarreraID, Estado: doc.Estado, TCarreraMs: math.MinInt64}
	if doc.TCarreraMs != nil {
		s.TCarreraMs = *doc.TCarreraMs
	}
	return s, true, nil
}

func (m *MongoFuente) Pilotos(ctx context.Context, carreraID string) ([]Piloto, error) {
	var doc struct {
		Pilotos []struct {
			Numero int    `bson:"numero"`
			Codigo string `bson:"codigo"`
			Nombre string `bson:"nombre"`
			Equipo string `bson:"equipo"`
			Color  string `bson:"color"`
		} `bson:"pilotos"`
	}
	err := m.carreras.FindOne(ctx, bson.M{"carrera_id": carreraID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil // sin datos de pilotos: el tablero muestra el numero
	}
	if err != nil {
		return nil, err
	}
	pilotos := make([]Piloto, 0, len(doc.Pilotos))
	for _, p := range doc.Pilotos {
		pilotos = append(pilotos, Piloto{Numero: p.Numero, Codigo: p.Codigo, Nombre: p.Nombre, Equipo: p.Equipo, Color: p.Color})
	}
	return pilotos, nil
}

func (m *MongoFuente) Posiciones(ctx context.Context, carreraID string, desde, hasta int64) ([]CambioPosicion, error) {
	cursor, err := m.eventos.Find(ctx,
		bson.M{
			"carrera_id":   carreraID,
			"tipo":         "posicion",
			"t_carrera_ms": bson.M{"$gt": desde, "$lte": hasta},
		},
		options.Find().
			SetSort(bson.D{{Key: "t_carrera_ms", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"t_carrera_ms": 1, "datos.raw.driver_number": 1, "datos.raw.position": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var cambios []CambioPosicion
	for cursor.Next(ctx) {
		var doc struct {
			TCarreraMs int64 `bson:"t_carrera_ms"`
			Datos      struct {
				Raw struct {
					Numero   float64 `bson:"driver_number"` // OpenF1 llega como JSON: numeros double
					Posicion float64 `bson:"position"`
				} `bson:"raw"`
			} `bson:"datos"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		cambios = append(cambios, CambioPosicion{
			TCarreraMs: doc.TCarreraMs,
			Numero:     int(doc.Datos.Raw.Numero),
			Posicion:   int(doc.Datos.Raw.Posicion),
		})
	}
	return cambios, cursor.Err()
}
