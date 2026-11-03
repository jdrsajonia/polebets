package visor

import (
	"context"
	"errors"
	"math"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// candidatasTrazado: cuantas vueltas rapidas se prueban si la primera no
	// tiene suficientes coordenadas (p. ej. un hueco en /location).
	candidatasTrazado = 5
	// minPuntosTrazado: menos que esto no dibuja una pista reconocible
	// (una vuelta trae ~360 puntos a ~3,7 Hz).
	minPuntosTrazado = 50
)

// MongoFuente lee las colecciones que escribe el Ingestor. Solo lectura (RNF-18).
type MongoFuente struct {
	simulaciones *mongo.Collection
	eventos      *mongo.Collection
}

// NuevaMongoFuente arma la fuente sobre la base del Motor.
func NuevaMongoFuente(db *mongo.Database) *MongoFuente {
	return &MongoFuente{simulaciones: db.Collection("simulaciones"), eventos: db.Collection("eventos_carrera")}
}

// SimulacionVigente: la programada o en curso (RF-22 permite solo una a la vez).
func (m *MongoFuente) SimulacionVigente(ctx context.Context) (Simulacion, bool, error) {
	var doc struct {
		ID         string `bson:"_id"`
		CarreraID  string `bson:"carrera_id"`
		Estado     string `bson:"estado"`
		TCarreraMs *int64 `bson:"t_carrera_ms"`
	}
	err := m.simulaciones.FindOne(ctx,
		bson.M{"estado": bson.M{"$in": bson.A{"programada", "en_curso"}}},
		options.FindOne().SetSort(bson.D{{Key: "hora_largada", Value: -1}}),
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

// Trazado toma la vuelta mas rapida sin salida de pits (desde la vuelta 2) y
// devuelve las coordenadas de ese piloto durante esa vuelta.
func (m *MongoFuente) Trazado(ctx context.Context, carreraID string) (Trazado, error) {
	cur, err := m.eventos.Find(ctx,
		bson.M{
			"carrera_id":               carreraID,
			"tipo":                     "vuelta",
			"datos.raw.is_pit_out_lap": false,
			"datos.raw.lap_number":     bson.M{"$gt": 1},
			"datos.raw.lap_duration":   bson.M{"$type": "number"},
		},
		options.Find().
			SetSort(bson.D{{Key: "datos.raw.lap_duration", Value: 1}}).
			SetLimit(candidatasTrazado).
			SetProjection(bson.M{"t_carrera_ms": 1, "datos.raw.driver_number": 1, "datos.raw.lap_number": 1, "datos.raw.lap_duration": 1}),
	)
	if err != nil {
		return Trazado{}, err
	}
	var vueltas []struct {
		T     int64 `bson:"t_carrera_ms"`
		Datos struct {
			Raw struct {
				Piloto   float64 `bson:"driver_number"` // OpenF1 llega como JSON: numeros double
				Vuelta   float64 `bson:"lap_number"`
				Duracion float64 `bson:"lap_duration"` // segundos
			} `bson:"raw"`
		} `bson:"datos"`
	}
	if err := cur.All(ctx, &vueltas); err != nil {
		return Trazado{}, err
	}
	for _, v := range vueltas {
		piloto := int(v.Datos.Raw.Piloto)
		fin := v.T + int64(math.Round(v.Datos.Raw.Duracion*1000))
		muestras, err := m.consultar(ctx, bson.M{
			"carrera_id":              carreraID,
			"tipo":                    "ubicacion",
			"datos.raw.driver_number": piloto,
			"t_carrera_ms":            bson.M{"$gte": v.T, "$lte": fin},
		})
		if err != nil {
			return Trazado{}, err
		}
		if len(muestras) < minPuntosTrazado {
			continue
		}
		puntos := make([]Punto, len(muestras))
		for i, mu := range muestras {
			puntos[i] = Punto{mu.X, mu.Y}
		}
		return Trazado{Piloto: piloto, Vuelta: int(v.Datos.Raw.Vuelta), Puntos: puntos}, nil
	}
	return Trazado{}, ErrSinTrazado
}

// Muestras devuelve las coordenadas de todos los autos en (desde, hasta].
func (m *MongoFuente) Muestras(ctx context.Context, carreraID string, desde, hasta int64) ([]Muestra, error) {
	return m.consultar(ctx, bson.M{
		"carrera_id":   carreraID,
		"tipo":         "ubicacion",
		"t_carrera_ms": bson.M{"$gt": desde, "$lte": hasta},
	})
}

// consultar lee eventos "ubicacion" ordenados por tiempo de carrera.
func (m *MongoFuente) consultar(ctx context.Context, filtro bson.M) ([]Muestra, error) {
	cur, err := m.eventos.Find(ctx, filtro,
		options.Find().
			SetSort(bson.D{{Key: "t_carrera_ms", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"t_carrera_ms": 1, "datos.raw.driver_number": 1, "datos.raw.x": 1, "datos.raw.y": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var muestras []Muestra
	for cur.Next(ctx) {
		var doc struct {
			T     int64 `bson:"t_carrera_ms"`
			Datos struct {
				Raw struct {
					Numero float64 `bson:"driver_number"`
					X      float64 `bson:"x"`
					Y      float64 `bson:"y"`
				} `bson:"raw"`
			} `bson:"datos"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		if doc.Datos.Raw.Numero <= 0 {
			continue
		}
		muestras = append(muestras, Muestra{Numero: int(doc.Datos.Raw.Numero), T: doc.T, X: doc.Datos.Raw.X, Y: doc.Datos.Raw.Y})
	}
	return muestras, cur.Err()
}
