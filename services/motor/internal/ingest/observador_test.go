package ingest

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// La ingesta guarda los numeros como int32/int64 y el seed (mongosh) como
// float64: los tres deben leerse igual.
func TestEntero(t *testing.T) {
	casos := []struct {
		valor  any
		quiero int
		ok     bool
	}{
		{int32(44), 44, true},
		{int64(44), 44, true},
		{float64(44), 44, true},
		{44, 44, true},
		{float64(4.5), 0, false},
		{"44", 0, false},
		{nil, 0, false},
	}
	for _, c := range casos {
		got, ok := entero(c.valor)
		if got != c.quiero || ok != c.ok {
			t.Errorf("entero(%#v) = %d, %v; quiero %d, %v", c.valor, got, ok, c.quiero, c.ok)
		}
	}
}

func TestPosicionDe(t *testing.T) {
	ev := EventoCarrera{Tipo: "posicion", Datos: map[string]any{"driver_number": int32(16), "position": float64(3)}}
	if n, p, ok := posicionDe(ev); !ok || n != 16 || p != 3 {
		t.Fatalf("posicionDe = %d, %d, %v", n, p, ok)
	}
	for _, ev := range []EventoCarrera{
		{Tipo: "vuelta", Datos: map[string]any{"driver_number": int32(1), "position": int32(1)}},
		{Tipo: "posicion", Datos: map[string]any{"driver_number": int32(1)}},
	} {
		if _, _, ok := posicionDe(ev); ok {
			t.Errorf("posicionDe(%+v) debio dar ok=false", ev)
		}
	}
}

// Prueba de integracion de EstadoInicial contra un MongoDB real. Se salta si
// no hay MOTOR_TEST_MONGO_URI. Usa una base aparte que borra al final; nunca
// toca apuestas_db. Ver "Pruebas" en el README del motor.
func TestEstadoInicialMongo(t *testing.T) {
	uri := os.Getenv("MOTOR_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("MOTOR_TEST_MONGO_URI no definida")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cliente, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer cliente.Disconnect(context.Background())
	db := cliente.Database("motor_test_hu05")
	_ = db.Drop(ctx)
	// defer (no t.Cleanup): tiene que correr antes del Disconnect de arriba.
	defer func() {
		if err := db.Drop(context.Background()); err != nil {
			t.Errorf("no se pudo borrar la base de prueba: %v", err)
		}
	}()

	base := time.Date(2024, 9, 1, 13, 0, 0, 0, time.UTC)
	desde := base.Add(time.Minute)
	pos := func(seg, numero, posicion int) any {
		return NormalizarPosicion("c1", PosicionOpenF1{
			Date: base.Add(time.Duration(seg) * time.Second), DriverNumber: numero, Position: posicion,
		})
	}
	docs := []any{
		pos(10, 1, 1), pos(10, 4, 2),
		pos(30, 4, 1), pos(31, 1, 2), // NOR pasa a VER antes de desde
		pos(59, 16, 3),
		pos(60, 1, 1), // justo en desde: ya es de la reproduccion, no cuenta
		pos(90, 16, 1),
		NormalizarPosicion("otra", PosicionOpenF1{Date: base, DriverNumber: 1, Position: 20}),
		NormalizarPilotos("c1", []PilotoOpenF1{
			{DriverNumber: 1, NameAcronym: "VER", FullName: "Max VERSTAPPEN", TeamName: "Red Bull Racing", TeamColour: "3671C6"},
			{DriverNumber: 4, NameAcronym: "NOR", FullName: "Lando NORRIS", TeamName: "McLaren", TeamColour: "FF8000"},
		}),
	}
	if _, err := db.Collection("eventos_carrera").InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	// Un evento del seed, con numeros float64 como los guarda mongosh.
	if _, err := db.Collection("eventos_carrera").InsertOne(ctx, bson.M{
		"carrera_id": "c1", "tipo": "posicion", "timestamp": base.Add(20 * time.Second),
		"datos": bson.M{"driver_number": float64(63), "position": float64(4)},
	}); err != nil {
		t.Fatal(err)
	}

	svc := NuevoServicio(db)
	pilotos, posiciones, err := svc.EstadoInicial(ctx, "c1", desde)
	if err != nil {
		t.Fatal(err)
	}
	quiero := map[int]int{1: 2, 4: 1, 16: 3, 63: 4}
	if len(posiciones) != len(quiero) {
		t.Fatalf("posiciones = %v, quiero %v", posiciones, quiero)
	}
	for numero, p := range quiero {
		if posiciones[numero] != p {
			t.Fatalf("posiciones = %v, quiero %v", posiciones, quiero)
		}
	}
	if len(pilotos) != 2 || pilotos[0] != (PilotoCarrera{Numero: 1, Codigo: "VER", Nombre: "Max VERSTAPPEN", Equipo: "Red Bull Racing", Color: "3671C6"}) {
		t.Fatalf("pilotos = %+v", pilotos)
	}

	// Desde cero: se reproduce desde el principio, sin posiciones previas.
	if _, posiciones, err := svc.EstadoInicial(ctx, "c1", time.Time{}); err != nil || len(posiciones) != 0 {
		t.Fatalf("desde cero: %v, %v", posiciones, err)
	}
	// Carrera sin lista_pilotos: no es error.
	if pilotos, _, err := svc.EstadoInicial(ctx, "otra", desde); err != nil || len(pilotos) != 0 {
		t.Fatalf("sin lista: %v, %v", pilotos, err)
	}
}
