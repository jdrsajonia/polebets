package estaticas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Estados de `simulaciones` (validador de db/mongo/init). El Gestor reacciona
// a la simulacion mas reciente que ya largo (programada aun no interesa para
// crear mercados estaticos: el evento se crea en cuanto EXISTE la simulacion).
const (
	SimProgramada = "programada"
	SimEnCurso    = "en_curso"
	SimFinalizada = "finalizada"
	SimFallida    = "fallida"
)

// Simulacion es lo que el worker necesita de `simulaciones`.
type Simulacion struct {
	ID          string
	CarreraID   string
	Estado      string
	HoraLargada time.Time
}

// Largo indica que la carrera ya arranco (o termino): los mercados estaticos
// se cierran a partir de aqui.
func (s Simulacion) Largo() bool {
	return s.Estado == SimEnCurso || s.Estado == SimFinalizada || s.Estado == SimFallida
}

// Termino indica que la carrera ya acabo: se resuelven las estaticas.
func (s Simulacion) Termino() bool {
	return s.Estado == SimFinalizada || s.Estado == SimFallida
}

// DatosCarrera es lo que el Gestor necesita de `carreras` para armar el
// mercado "ganador": los pilotos (numero -> codigo de 3 letras).
type DatosCarrera struct {
	Pilotos map[int]string
}

// Fuente es lo que el worker lee de Mongo. Solo lectura (RNF-18).
type Fuente interface {
	// SimulacionActual es la mas reciente en cualquier estado; ok=false si no
	// hay ninguna. El evento estatico se crea apenas aparece.
	SimulacionActual(ctx context.Context) (Simulacion, bool, error)
	Carrera(ctx context.Context, carreraID string) (DatosCarrera, error)
	// Posiciones devuelve todos los eventos de posicion de la carrera,
	// ordenados por t_carrera_ms. Se usa al final para resolver el ganador.
	Posiciones(ctx context.Context, carreraID string) ([]EventoPosicion, error)
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
	ID          string    `bson:"_id"`
	CarreraID   string    `bson:"carrera_id"`
	Estado      string    `bson:"estado"`
	HoraLargada time.Time `bson:"hora_largada"`
}

func (l *LectorMongo) SimulacionActual(ctx context.Context) (Simulacion, bool, error) {
	var doc docSimulacion
	err := l.simulaciones.FindOne(ctx,
		bson.M{"estado": bson.M{"$in": bson.A{SimProgramada, SimEnCurso, SimFinalizada, SimFallida}}},
		options.FindOne().
			SetSort(bson.D{{Key: "creada_en", Value: -1}}).
			SetProjection(bson.M{"carrera_id": 1, "estado": 1, "hora_largada": 1}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Simulacion{}, false, nil
	}
	if err != nil {
		return Simulacion{}, false, fmt.Errorf("leyendo la simulacion actual: %w", err)
	}
	return Simulacion{
		ID: doc.ID, CarreraID: doc.CarreraID, Estado: doc.Estado, HoraLargada: doc.HoraLargada.UTC(),
	}, true, nil
}

type docCarrera struct {
	Pilotos []struct {
		Numero int    `bson:"numero"`
		Codigo string `bson:"codigo"`
	} `bson:"pilotos"`
}

func (l *LectorMongo) Carrera(ctx context.Context, carreraID string) (DatosCarrera, error) {
	var doc docCarrera
	err := l.carreras.FindOne(ctx, bson.M{"carrera_id": carreraID},
		options.FindOne().SetProjection(bson.M{"pilotos.numero": 1, "pilotos.codigo": 1}),
	).Decode(&doc)
	if err != nil {
		return DatosCarrera{}, fmt.Errorf("leyendo la carrera %s: %w", carreraID, err)
	}
	d := DatosCarrera{Pilotos: make(map[int]string, len(doc.Pilotos))}
	for _, p := range doc.Pilotos {
		d.Pilotos[p.Numero] = p.Codigo
	}
	return d, nil
}

type docPosicion struct {
	T     int64 `bson:"t_carrera_ms"`
	Datos struct {
		Raw struct {
			Piloto int `bson:"driver_number"`
			Pos    int `bson:"position"`
		} `bson:"raw"`
	} `bson:"datos"`
}

func (l *LectorMongo) Posiciones(ctx context.Context, carreraID string) ([]EventoPosicion, error) {
	cur, err := l.eventos.Find(ctx,
		bson.M{"carrera_id": carreraID, "tipo": "posicion"},
		options.Find().
			SetSort(bson.D{{Key: "t_carrera_ms", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"t_carrera_ms": 1, "datos.raw.driver_number": 1, "datos.raw.position": 1}),
	)
	if err != nil {
		return nil, fmt.Errorf("leyendo posiciones de %s: %w", carreraID, err)
	}
	defer cur.Close(ctx)
	var pos []EventoPosicion
	for cur.Next(ctx) {
		var doc docPosicion
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decodificando una posicion: %w", err)
		}
		if doc.Datos.Raw.Piloto == 0 {
			continue // sin piloto no sirve
		}
		pos = append(pos, EventoPosicion{T: doc.T, Piloto: doc.Datos.Raw.Piloto, Pos: doc.Datos.Raw.Pos})
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("leyendo posiciones de %s: %w", carreraID, err)
	}
	return pos, nil
}
