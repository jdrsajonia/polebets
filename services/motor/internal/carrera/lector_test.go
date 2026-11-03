package carrera

import (
	"context"
	"errors"
	"math"
	"testing"
)

// fuenteFalsa imita lo que el Ingestor deja en Mongo.
type fuenteFalsa struct {
	sim       Simulacion
	haySim    bool
	pilotos   map[string][]Piloto
	eventos   map[string][]CambioPosicion // por carrera, ordenados por t
	fallar    bool
	consultas [][2]int64
}

func (f *fuenteFalsa) SimulacionActual(context.Context) (Simulacion, bool, error) {
	if f.fallar {
		return Simulacion{}, false, errors.New("mongo caido")
	}
	return f.sim, f.haySim, nil
}

func (f *fuenteFalsa) Pilotos(_ context.Context, carreraID string) ([]Piloto, error) {
	return f.pilotos[carreraID], nil
}

func (f *fuenteFalsa) Posiciones(_ context.Context, carreraID string, desde, hasta int64) ([]CambioPosicion, error) {
	f.consultas = append(f.consultas, [2]int64{desde, hasta})
	var out []CambioPosicion
	for _, e := range f.eventos[carreraID] {
		if e.TCarreraMs > desde && e.TCarreraMs <= hasta {
			out = append(out, e)
		}
	}
	return out, nil
}

type tableroFalso struct {
	inicios    []string
	pilotos    []Piloto
	posiciones map[int]int
	vuelta     Vuelta
	fotos      int // difusiones por Aplicar
}

func (t *tableroFalso) Iniciar(carreraID string, pilotos []Piloto) {
	t.inicios = append(t.inicios, carreraID)
	t.pilotos = pilotos
	t.posiciones = map[int]int{}
}

func (t *tableroFalso) Aplicar(cambios map[int]int, vuelta Vuelta) {
	for n, p := range cambios {
		t.posiciones[n] = p
	}
	t.vuelta = vuelta
	t.fotos++
}

func nueva() (*fuenteFalsa, *tableroFalso, *Lector) {
	f := &fuenteFalsa{
		haySim:  true,
		sim:     Simulacion{ID: "sim-1", CarreraID: "9158", Estado: "en_curso", TCarreraMs: 0},
		pilotos: map[string][]Piloto{"9158": {{Numero: 1, Codigo: "VER"}, {Numero: 4, Codigo: "NOR"}, {Numero: 16, Codigo: "LEC"}}},
		eventos: map[string][]CambioPosicion{"9158": {
			{TCarreraMs: -60000, Numero: 1, Posicion: 1}, // parrilla, antes de la largada
			{TCarreraMs: -60000, Numero: 4, Posicion: 2},
			{TCarreraMs: -60000, Numero: 16, Posicion: 3},
			{TCarreraMs: 5000, Numero: 4, Posicion: 1}, // NOR pasa a VER
			{TCarreraMs: 5000, Numero: 1, Posicion: 2},
			{TCarreraMs: 9000, Numero: 16, Posicion: 2}, // LEC pasa a VER
			{TCarreraMs: 9000, Numero: 1, Posicion: 3},
		}},
	}
	tb := &tableroFalso{}
	return f, tb, NuevoLector(f, tb, 0)
}

func paso(t *testing.T, l *Lector) {
	t.Helper()
	if err := l.Paso(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestArrancaConLosPilotosYLaParrilla(t *testing.T) {
	_, tb, l := nueva()

	paso(t, l)

	if len(tb.inicios) != 1 || tb.inicios[0] != "9158" || len(tb.pilotos) != 3 {
		t.Fatalf("inicio = %v pilotos %v", tb.inicios, tb.pilotos)
	}
	if want := map[int]int{1: 1, 4: 2, 16: 3}; !igual(tb.posiciones, want) || tb.fotos != 1 {
		t.Errorf("parrilla = %v (fotos %d)", tb.posiciones, tb.fotos)
	}
}

func TestAvanzaConElRelojYDifundeUnaFotoPorPaso(t *testing.T) {
	f, tb, l := nueva()
	paso(t, l)

	f.sim.TCarreraMs = 9500 // el Ingestor avanzo dos tramos
	paso(t, l)

	if want := map[int]int{4: 1, 16: 2, 1: 3}; !igual(tb.posiciones, want) {
		t.Errorf("posiciones = %v", tb.posiciones)
	}
	if tb.fotos != 2 {
		t.Errorf("debia difundir una foto por paso; fotos = %d", tb.fotos)
	}
}

func TestSinAvanceNoConsultaNiDifunde(t *testing.T) {
	f, tb, l := nueva()
	paso(t, l)
	consultas := len(f.consultas)

	paso(t, l) // mismo t_carrera_ms

	if len(f.consultas) != consultas || tb.fotos != 1 {
		t.Errorf("consultas %d -> %d, fotos %d", consultas, len(f.consultas), tb.fotos)
	}
}

func TestNoRepiteEventosYaAplicados(t *testing.T) {
	f, _, l := nueva()
	paso(t, l)
	f.sim.TCarreraMs = 6000
	paso(t, l)

	if got := f.consultas[1]; got != [2]int64{0, 6000} {
		t.Errorf("segunda consulta = %v, esperaba (0, 6000]", got)
	}
	if got := f.consultas[0]; got[0] != math.MinInt64 {
		t.Errorf("la primera consulta debia incluir la parrilla: %v", got)
	}
}

func TestSimulacionNuevaReiniciaElTablero(t *testing.T) {
	f, tb, l := nueva()
	f.sim.TCarreraMs = 9500
	paso(t, l)

	f.sim = Simulacion{ID: "sim-2", CarreraID: "9999", Estado: "en_curso", TCarreraMs: 100}
	f.pilotos["9999"] = []Piloto{{Numero: 44, Codigo: "HAM"}}
	f.eventos["9999"] = []CambioPosicion{{TCarreraMs: 50, Numero: 44, Posicion: 1}}
	paso(t, l)

	if len(tb.inicios) != 2 || tb.inicios[1] != "9999" || !igual(tb.posiciones, map[int]int{44: 1}) {
		t.Errorf("inicios %v posiciones %v", tb.inicios, tb.posiciones)
	}
}

func TestSimulacionQueAunNoArrancaNoLeeEventos(t *testing.T) {
	f, tb, l := nueva()
	f.sim.TCarreraMs = math.MinInt64 // en_curso pero sin el primer tick

	paso(t, l)

	if len(tb.inicios) != 1 || tb.fotos != 0 || len(f.consultas) != 0 {
		t.Errorf("inicios %v fotos %d consultas %d", tb.inicios, tb.fotos, len(f.consultas))
	}
}

func TestSinSimulacionOConErrorNoTocaElTablero(t *testing.T) {
	f, tb, l := nueva()
	f.haySim = false
	paso(t, l)
	if len(tb.inicios) != 0 {
		t.Errorf("sin simulacion no debia iniciar")
	}

	f.haySim, f.fallar = true, true
	if err := l.Paso(context.Background()); err == nil {
		t.Error("esperaba error")
	}
	f.fallar = false
	paso(t, l) // se recupera en el siguiente paso
	if len(tb.inicios) != 1 || tb.fotos != 1 {
		t.Errorf("tras recuperarse: inicios %v fotos %d", tb.inicios, tb.fotos)
	}
}

func TestLaFotoLlevaLaVueltaActualYElTotal(t *testing.T) {
	f, tb, l := nueva()
	f.sim.Vuelta = Vuelta{Actual: 1, Total: 57}
	paso(t, l)
	if tb.vuelta != (Vuelta{Actual: 1, Total: 57}) {
		t.Errorf("vuelta = %+v", tb.vuelta)
	}

	f.sim.TCarreraMs = 9500
	f.sim.Vuelta = Vuelta{Actual: 2, Total: 57}
	paso(t, l)
	if tb.vuelta != (Vuelta{Actual: 2, Total: 57}) || tb.fotos != 2 {
		t.Errorf("vuelta = %+v fotos %d", tb.vuelta, tb.fotos)
	}
}

func TestSoloCambiaLaVueltaTambienDifunde(t *testing.T) {
	f, tb, l := nueva()
	f.sim.Vuelta = Vuelta{Actual: 1, Total: 57}
	// Primer paso: parrilla + vuelta 1.
	paso(t, l)
	f.sim.TCarreraMs = 1000 // sin eventos de posicion en (0, 1000]
	f.sim.Vuelta = Vuelta{Actual: 2, Total: 57}

	paso(t, l)

	if tb.fotos != 2 || tb.vuelta.Actual != 2 {
		t.Errorf("fotos %d vuelta %+v", tb.fotos, tb.vuelta)
	}
	f.sim.TCarreraMs = 2000 // ni posiciones ni vuelta nuevas: no se difunde
	paso(t, l)
	if tb.fotos != 2 {
		t.Errorf("sin cambios no debia difundir; fotos %d", tb.fotos)
	}
}

func igual(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range b {
		if a[k] != v {
			return false
		}
	}
	return true
}
