package dinamicas

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// coleccionFalsa guarda el filtro y las opciones y devuelve documentos bson
// armados a mano, asi se prueba la consulta y el mapeo sin un Mongo real.
type coleccionFalsa struct {
	docs   []any
	err    error
	filtro any
	uno    *options.FindOneOptions
	varios *options.FindOptions
}

func (c *coleccionFalsa) FindOne(_ context.Context, filtro any, opts ...*options.FindOneOptions) *mongo.SingleResult {
	c.filtro, c.uno = filtro, options.MergeFindOneOptions(opts...)
	if c.err != nil || len(c.docs) == 0 {
		err := c.err
		if err == nil {
			err = mongo.ErrNoDocuments
		}
		return mongo.NewSingleResultFromDocument(bson.D{}, err, nil)
	}
	return mongo.NewSingleResultFromDocument(c.docs[0], nil, nil)
}

func (c *coleccionFalsa) Find(_ context.Context, filtro any, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	c.filtro, c.varios = filtro, options.MergeFindOptions(opts...)
	if c.err != nil {
		return nil, c.err
	}
	return mongo.NewCursorFromDocuments(c.docs, nil, nil)
}

func TestSimulacionActual(t *testing.T) {
	iniciada := time.Date(2026, 11, 2, 20, 0, 0, 123_000_000, time.UTC)
	casos := []struct {
		nombre string
		doc    bson.D
		ok     bool
		want   Simulacion
	}{
		{"en curso con latido", bson.D{
			{Key: "_id", Value: "sim-1"}, {Key: "carrera_id", Value: "9165"}, {Key: "estado", Value: "en_curso"},
			{Key: "t_inicio_ms", Value: int64(0)}, {Key: "t_carrera_ms", Value: int64(61000)},
			{Key: "iniciada_en", Value: primitive.NewDateTimeFromTime(iniciada)},
		}, true, Simulacion{ID: "sim-1", CarreraID: "9165", Estado: SimEnCurso, TCarreraMs: 61000, IniciadaEn: iniciada}},
		{"sin latido todavia", bson.D{
			{Key: "_id", Value: "sim-2"}, {Key: "carrera_id", Value: "9165"}, {Key: "estado", Value: "en_curso"},
			{Key: "t_inicio_ms", Value: int64(600000)}, {Key: "iniciada_en", Value: primitive.NewDateTimeFromTime(iniciada)},
		}, true, Simulacion{ID: "sim-2", CarreraID: "9165", Estado: SimEnCurso, TInicioMs: 600000, TCarreraMs: math.MinInt64, IniciadaEn: iniciada}},
		{"no hay ninguna", nil, false, Simulacion{}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			col := &coleccionFalsa{}
			if c.doc != nil {
				col.docs = []any{c.doc}
			}
			s, ok, err := (&LectorMongo{simulaciones: col}).SimulacionActual(context.Background())
			if err != nil || ok != c.ok || !reflect.DeepEqual(s, c.want) {
				t.Fatalf("s = %+v, ok = %v, err = %v", s, ok, err)
			}
			want := bson.M{"estado": bson.M{"$in": bson.A{"en_curso", "finalizada", "fallida"}}}
			if !reflect.DeepEqual(col.filtro, want) {
				t.Errorf("filtro = %v", col.filtro)
			}
			if !reflect.DeepEqual(col.uno.Sort, bson.D{{Key: "iniciada_en", Value: -1}}) {
				t.Errorf("sort = %v", col.uno.Sort)
			}
		})
	}
	t.Run("error de Mongo", func(t *testing.T) {
		col := &coleccionFalsa{err: errors.New("se cayo")}
		if _, _, err := (&LectorMongo{simulaciones: col}).SimulacionActual(context.Background()); err == nil {
			t.Fatal("se esperaba error")
		}
	})
}

func TestSimulacionHoraRealYTermino(t *testing.T) {
	ini := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	s := Simulacion{TInicioMs: 600000, IniciadaEn: ini, Estado: SimEnCurso}
	if got := s.HoraReal(630500); !got.Equal(ini.Add(30500*time.Millisecond)) || got.Location() != time.UTC {
		t.Fatalf("hora = %v", got)
	}
	if s.Termino() {
		t.Error("en_curso no termino")
	}
	for _, e := range []string{SimFinalizada, SimFallida} {
		if !(Simulacion{Estado: e}).Termino() {
			t.Errorf("%s deberia terminar", e)
		}
	}
}

func TestCarrera(t *testing.T) {
	col := &coleccionFalsa{docs: []any{bson.D{
		{Key: "total_vueltas", Value: int32(53)},
		{Key: "pilotos", Value: bson.A{
			bson.D{{Key: "numero", Value: int32(1)}, {Key: "codigo", Value: "VER"}},
			bson.D{{Key: "numero", Value: int32(4)}, {Key: "codigo", Value: ""}},
		}},
	}}}
	d, err := (&LectorMongo{carreras: col}).Carrera(context.Background(), "9165")
	if err != nil {
		t.Fatal(err)
	}
	if d.TotalVueltas != 53 || !reflect.DeepEqual(d.Pilotos, map[int]string{1: "VER", 4: ""}) {
		t.Fatalf("datos = %+v", d)
	}
	if !reflect.DeepEqual(col.filtro, bson.M{"carrera_id": "9165"}) {
		t.Errorf("filtro = %v", col.filtro)
	}
	if _, err := (&LectorMongo{carreras: &coleccionFalsa{}}).Carrera(context.Background(), "x"); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("sin carrera: err = %v", err)
	}
}

func TestEventosConsulta(t *testing.T) {
	col := &coleccionFalsa{}
	if _, err := (&LectorMongo{eventos: col}).Eventos(context.Background(), "9165", math.MinInt64, 5000); err != nil {
		t.Fatal(err)
	}
	f := col.filtro.(bson.M)
	if f["carrera_id"] != "9165" {
		t.Errorf("carrera = %v", f["carrera_id"])
	}
	if !reflect.DeepEqual(f["tipo"], bson.M{"$in": bson.A{"posicion", "intervalo", "vuelta", "stint", "pit", "control"}}) {
		t.Errorf("tipos = %v", f["tipo"])
	}
	if !reflect.DeepEqual(f["t_carrera_ms"], bson.M{"$gt": int64(math.MinInt64), "$lte": int64(5000)}) {
		t.Errorf("rango = %v", f["t_carrera_ms"])
	}
	if !reflect.DeepEqual(col.varios.Sort, bson.D{{Key: "t_carrera_ms", Value: 1}, {Key: "_id", Value: 1}}) {
		t.Errorf("sort = %v", col.varios.Sort)
	}
	p := col.varios.Projection.(bson.M)
	for _, campo := range []string{"tipo", "t_carrera_ms", "datos.raw.interval", "datos.raw.lap_duration", "datos.raw.message"} {
		if p[campo] != 1 {
			t.Errorf("falta %s en la proyeccion", campo)
		}
	}
	for _, prohibido := range []string{"datos.raw.lap_end", "datos.raw.pit_duration", "datos"} {
		if _, ok := p[prohibido]; ok {
			t.Errorf("la proyeccion no debe traer %s", prohibido)
		}
	}
}

func TestEventosDecodificacion(t *testing.T) {
	oid := primitive.NewObjectIDFromTimestamp(time.Unix(1_700_000_000, 0))
	evento := func(id any, tipo string, tms int64, raw bson.D) bson.D {
		return bson.D{{Key: "_id", Value: id}, {Key: "tipo", Value: tipo}, {Key: "t_carrera_ms", Value: tms},
			{Key: "datos", Value: bson.D{{Key: "raw", Value: raw}}}}
	}
	col := &coleccionFalsa{docs: []any{
		// OpenF1 llega como JSON: los numeros quedan como double.
		evento(oid, "posicion", 1000, bson.D{{Key: "driver_number", Value: 1.0}, {Key: "position", Value: 2.0}}),
		evento(oid, "intervalo", 2000, bson.D{{Key: "driver_number", Value: int32(4)}, {Key: "interval", Value: nil}, {Key: "gap_to_leader", Value: "+1 LAP"}}),
		evento("id-texto", "intervalo", 3000, bson.D{{Key: "driver_number", Value: int64(16)}, {Key: "interval", Value: 0.512}}),
		evento(oid, "vuelta", 4000, bson.D{{Key: "driver_number", Value: 1.0}, {Key: "lap_number", Value: 3.0}, {Key: "lap_duration", Value: 92.345}}),
		evento(oid, "control", 5000, bson.D{{Key: "category", Value: "Flag"}, {Key: "flag", Value: "YELLOW"}, {Key: "message", Value: "YELLOW IN TRACK SECTOR 2"}}),
		evento(oid, "posicion", 6000, bson.D{{Key: "position", Value: 3.0}}), // sin piloto: se descarta
	}}
	evs, err := (&LectorMongo{eventos: col}).Eventos(context.Background(), "9165", 0, 6000)
	if err != nil {
		t.Fatal(err)
	}
	want := []Evento{
		{ID: oid.Hex(), T: 1000, Tipo: TipoPosicion, Piloto: 1, Posicion: 2},
		{ID: oid.Hex(), T: 2000, Tipo: TipoIntervalo, Piloto: 4},
		{ID: "id-texto", T: 3000, Tipo: TipoIntervalo, Piloto: 16, Intervalo: f(0.512)},
		{ID: oid.Hex(), T: 4000, Tipo: TipoVuelta, Piloto: 1, Vuelta: 3, DuracionVuelta: f(92.345)},
		{ID: oid.Hex(), T: 5000, Tipo: TipoControl, Categoria: "Flag", Bandera: "YELLOW", Mensaje: "YELLOW IN TRACK SECTOR 2"},
	}
	if !reflect.DeepEqual(evs, want) {
		t.Fatalf("eventos =\n%+v\nse esperaba\n%+v", evs, want)
	}
}

func TestTextoIDConservaElOrden(t *testing.T) {
	raw := func(v any) bson.RawValue {
		tipo, b, err := bson.MarshalValue(v)
		if err != nil {
			t.Fatal(err)
		}
		return bson.RawValue{Type: tipo, Value: b}
	}
	a := primitive.NewObjectIDFromTimestamp(time.Unix(1_700_000_000, 0))
	b := primitive.NewObjectIDFromTimestamp(time.Unix(1_700_000_001, 0))
	ia, ib := textoID(raw(a)), textoID(raw(b))
	if ia != a.Hex() || !(ia < ib) {
		t.Fatalf("%s deberia ir antes que %s", ia, ib)
	}
	if textoID(raw("abc")) != "abc" || textoID(bson.RawValue{}) != "" {
		t.Fatal("ids de texto o vacios")
	}
}
