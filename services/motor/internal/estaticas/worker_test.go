package estaticas

import (
	"context"
	"testing"
)

// fuenteFalsa devuelve una simulacion y una carrera fijas. El test cambia el
// estado de la simulacion entre ticks.
type fuenteFalsa struct {
	sim        Simulacion
	pilotos    map[int]string
	posiciones []EventoPosicion
}

func (f *fuenteFalsa) SimulacionActual(context.Context) (Simulacion, bool, error) {
	if f.sim.ID == "" {
		return Simulacion{}, false, nil
	}
	return f.sim, true, nil
}
func (f *fuenteFalsa) Carrera(context.Context, string) (DatosCarrera, error) {
	return DatosCarrera{Pilotos: f.pilotos}, nil
}
func (f *fuenteFalsa) Posiciones(context.Context, string) ([]EventoPosicion, error) {
	return f.posiciones, nil
}

// pagosFalso cuenta las llamadas para verificar el ciclo y la idempotencia.
type pagosFalso struct {
	eventosCreados  int
	mercadosCreados int
	cerrados        int
	largadas        int
	resueltos       int
	ultimaGanadora  string
	jugadores       []string
}

func (p *pagosFalso) CrearEvento(context.Context, AltaEvento) (bool, error) {
	p.eventosCreados++
	return true, nil
}
func (p *pagosFalso) CrearMercado(context.Context, AltaMercado) (bool, error) {
	p.mercadosCreados++
	return true, nil
}
func (p *pagosFalso) Cerrar(context.Context, string) error { p.cerrados++; return nil }
func (p *pagosFalso) Resolver(_ context.Context, _, opcion string) error {
	p.resueltos++
	p.ultimaGanadora = opcion
	return nil
}
func (p *pagosFalso) Largar(context.Context, string) (Largada, error) {
	p.largadas++
	return Largada{Jugadores: p.jugadores, FichasIniciales: 1000}, nil
}

// torneoFalso registra la inicializacion de fichas.
type torneoFalso struct {
	veces     int
	jugadores []string
}

func (t *torneoFalso) InicializarTorneo(_ context.Context, _ string, jugadores []string, _ int64) error {
	t.veces++
	t.jugadores = jugadores
	return nil
}

func TestCicloCompleto(t *testing.T) {
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-1", CarreraID: "2024_monza", Estado: SimProgramada},
		pilotos:    map[int]string{1: "VER", 4: "NOR", 44: "HAM", 16: "LEC"},
		posiciones: []EventoPosicion{{T: 9000, Piloto: 4, Pos: 1}, {T: 9000, Piloto: 1, Pos: 2}},
	}
	pagos := &pagosFalso{jugadores: []string{"u1", "u2", "u3", "u4"}}
	torneo := &torneoFalso{}
	w := NuevoWorker(fuente, pagos, torneo)
	ctx := context.Background()

	// Tick 1: programada -> crea evento y abre mercado.
	must(t, w.Procesar(ctx))
	if pagos.eventosCreados != 1 || pagos.mercadosCreados != 1 {
		t.Fatalf("tras programada: eventos=%d mercados=%d, se esperaba 1 y 1", pagos.eventosCreados, pagos.mercadosCreados)
	}
	if pagos.cerrados != 0 || pagos.resueltos != 0 {
		t.Fatalf("no debia cerrar ni resolver aun")
	}

	// Tick 2: sigue programada -> no repite nada (idempotencia en memoria).
	must(t, w.Procesar(ctx))
	if pagos.eventosCreados != 1 || pagos.mercadosCreados != 1 {
		t.Fatalf("un segundo tick en programada no debia recrear nada")
	}

	// Tick 3: en_curso (largada) -> cierra mercado, larga e inicializa fichas.
	fuente.sim.Estado = SimEnCurso
	must(t, w.Procesar(ctx))
	if pagos.cerrados != 1 || pagos.largadas != 1 {
		t.Fatalf("tras la largada: cerrados=%d largadas=%d, se esperaba 1 y 1", pagos.cerrados, pagos.largadas)
	}
	if torneo.veces != 1 || len(torneo.jugadores) != 4 {
		t.Fatalf("fichas: veces=%d jugadores=%d, se esperaba 1 y 4", torneo.veces, len(torneo.jugadores))
	}

	// Tick 4: finalizada -> resuelve con el ganador (NOR, ultimo position==1).
	fuente.sim.Estado = SimFinalizada
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 1 || pagos.ultimaGanadora != "NOR" {
		t.Fatalf("resolucion: resueltos=%d ganadora=%q, se esperaba 1 y NOR", pagos.resueltos, pagos.ultimaGanadora)
	}

	// Tick 5: sigue finalizada -> no resuelve de nuevo.
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 1 {
		t.Fatalf("un segundo tick en finalizada no debia resolver otra vez (resueltos=%d)", pagos.resueltos)
	}
}

func TestArranqueConCarreraYaTerminada(t *testing.T) {
	// El Motor arranca con la simulacion ya finalizada: debe ponerse al dia en
	// un solo recorrido (crear, largar, resolver).
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-2", CarreraID: "2024_monza", Estado: SimFinalizada},
		pilotos:    map[int]string{1: "VER", 4: "NOR"},
		posiciones: []EventoPosicion{{T: 100, Piloto: 1, Pos: 1}},
	}
	pagos := &pagosFalso{jugadores: nil} // torneo cancelado: sin jugadores
	w := NuevoWorker(fuente, pagos, &torneoFalso{})

	must(t, w.Procesar(context.Background()))

	if pagos.eventosCreados != 1 || pagos.mercadosCreados != 1 || pagos.cerrados != 1 || pagos.largadas != 1 || pagos.resueltos != 1 {
		t.Fatalf("recuperacion incompleta: %+v", pagos)
	}
	if pagos.ultimaGanadora != "VER" {
		t.Errorf("ganadora = %q, se esperaba VER", pagos.ultimaGanadora)
	}
}

func TestSinSimulacionNoHaceNada(t *testing.T) {
	pagos := &pagosFalso{}
	w := NuevoWorker(&fuenteFalsa{}, pagos, nil)
	must(t, w.Procesar(context.Background()))
	if pagos.eventosCreados != 0 {
		t.Errorf("sin simulacion no debia crear nada")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
}
