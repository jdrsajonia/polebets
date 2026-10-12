package ingest

// Contrato entre la reproduccion y quien muestra las posiciones (HU-05).
//
// ingest no sabe nada de la tabla de posiciones ni del WebSocket: solo avisa
// a un ObservadorCarrera cuando arranca una reproduccion y cada vez que un
// piloto cambia de posicion. En main.go se le pasa el difusor del paquete
// posiciones.

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

// PilotoCarrera son los datos fijos de un piloto, sacados del evento
// lista_pilotos de la ingesta.
type PilotoCarrera struct {
	Numero int
	Codigo string // name_acronym, p. ej. "VER"
	Nombre string // full_name
	Equipo string // team_name
	Color  string // team_colour: hex sin "#", "" si OpenF1 no lo dio
}

// ObservadorCarrera recibe lo que pasa en la reproduccion.
type ObservadorCarrera interface {
	// Iniciar arranca una carrera nueva: los pilotos y la posicion de cada uno
	// (por numero de carro) en el punto donde empieza la reproduccion.
	Iniciar(carreraID string, pilotos []PilotoCarrera, posiciones map[int]int)
	// Actualizar aplica un evento de posicion de la reproduccion.
	Actualizar(numero, posicion int)
}

// EstadoInicial reconstruye la carrera en el instante desde, porque la
// reproduccion suele empezar a mitad de carrera:
//
//   - pilotos: la lista_pilotos de la carrera (vacia si no se ingirio).
//   - posiciones: para cada piloto, su ULTIMA posicion con timestamp < desde.
//     Con desde cero (reproducir desde el principio) queda vacia y se llena
//     con los primeros eventos.
func (s *Servicio) EstadoInicial(ctx context.Context, carreraID string, desde time.Time) ([]PilotoCarrera, map[int]int, error) {
	pilotos, err := s.pilotos(ctx, carreraID)
	if err != nil {
		return nil, nil, fmt.Errorf("leer lista de pilotos: %w", err)
	}
	posiciones := map[int]int{}
	if desde.IsZero() {
		return pilotos, posiciones, nil
	}

	cur, err := s.eventos.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"carrera_id": carreraID,
			"tipo":       "posicion",
			"timestamp":  bson.M{"$lt": desde},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: 1}, {Key: "_id", Value: 1}}}},
		{{Key: "$group", Value: bson.M{
			"_id":      "$datos.driver_number",
			"posicion": bson.M{"$last": "$datos.position"},
		}}},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("posiciones previas: %w", err)
	}
	var filas []struct {
		Numero   any `bson:"_id"`
		Posicion any `bson:"posicion"`
	}
	if err := cur.All(ctx, &filas); err != nil {
		return nil, nil, fmt.Errorf("posiciones previas: %w", err)
	}
	for _, f := range filas {
		numero, ok1 := entero(f.Numero)
		posicion, ok2 := entero(f.Posicion)
		if ok1 && ok2 {
			posiciones[numero] = posicion
		}
	}
	return pilotos, posiciones, nil
}

// pilotos lee la lista_pilotos que guardo la ingesta (NormalizarPilotos).
func (s *Servicio) pilotos(ctx context.Context, carreraID string) ([]PilotoCarrera, error) {
	var doc struct {
		Datos struct {
			Pilotos []struct {
				DriverNumber any    `bson:"driver_number"`
				FullName     string `bson:"full_name"`
				NameAcronym  string `bson:"name_acronym"`
				TeamName     string `bson:"team_name"`
				TeamColour   string `bson:"team_colour"`
			} `bson:"pilotos"`
		} `bson:"datos"`
	}
	err := s.eventos.FindOne(ctx,
		bson.M{"carrera_id": carreraID, "tipo": "vuelta", "datos.tipo_vuelta": "lista_pilotos"},
		options.FindOne().SetProjection(bson.M{"datos.pilotos": 1}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil // sin lista: la tabla sale con numeros en vez de nombres
	}
	if err != nil {
		return nil, err
	}
	pilotos := make([]PilotoCarrera, 0, len(doc.Datos.Pilotos))
	for _, p := range doc.Datos.Pilotos {
		numero, ok := entero(p.DriverNumber)
		if !ok {
			continue
		}
		pilotos = append(pilotos, PilotoCarrera{
			Numero: numero, Codigo: p.NameAcronym, Nombre: p.FullName,
			Equipo: p.TeamName, Color: p.TeamColour,
		})
	}
	return pilotos, nil
}

// posicionDe saca (numero, posicion) de un evento tipo "posicion".
func posicionDe(ev EventoCarrera) (numero, posicion int, ok bool) {
	if ev.Tipo != "posicion" {
		return 0, 0, false
	}
	numero, ok1 := entero(ev.Datos["driver_number"])
	posicion, ok2 := entero(ev.Datos["position"])
	return numero, posicion, ok1 && ok2
}

// entero convierte un numero leido de MongoDB a int. Llega como int32 o
// int64 si lo guardo la ingesta (Go) y como float64 si lo guardo el seed
// (mongosh/JavaScript). Un float con decimales no es un numero valido aqui.
func entero(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != math.Trunc(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}
