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
	finalizados     int
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
func (p *pagosFalso) Finalizar(context.Context, string) error { p.finalizados++; return nil }

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
	// Torneo activo (4 inscritos): HU2-12 cierra con reparto, no este worker.
	if pagos.finalizados != 0 {
		t.Fatalf("un torneo activo NO debe finalizarse desde estaticas (finalizados=%d)", pagos.finalizados)
	}
}

func TestTorneoCanceladoFinalizaLaCarrera(t *testing.T) {
	// Largada sin jugadores (torneo cancelado por pocos inscritos). Al terminar
	// la carrera, estaticas finaliza el evento en Pagos (players vacios): nadie
	// mas lo cerraria, porque HU2-12 solo cierra torneos activos (Redis).
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-5", CarreraID: "2024_monza", Estado: SimProgramada},
		pilotos:    map[int]string{1: "VER", 4: "NOR"},
		posiciones: []EventoPosicion{{T: 100, Piloto: 1, Pos: 1}},
	}
	pagos := &pagosFalso{jugadores: nil} // largada cancela el torneo
	torneo := &torneoFalso{}
	w := NuevoWorker(fuente, pagos, torneo)
	ctx := context.Background()

	must(t, w.Procesar(ctx)) // programada: crea y abre

	// en_curso: larga (sin jugadores). No inicializa fichas ni finaliza aun.
	fuente.sim.Estado = SimEnCurso
	must(t, w.Procesar(ctx))
	if torneo.veces != 0 {
		t.Fatalf("torneo cancelado no debe inicializar fichas (veces=%d)", torneo.veces)
	}
	if pagos.finalizados != 0 {
		t.Fatalf("no debe finalizar antes de que termine la carrera (finalizados=%d)", pagos.finalizados)
	}

	// finalizada: resuelve y finaliza el evento (torneo cancelado).
	fuente.sim.Estado = SimFinalizada
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 1 {
		t.Fatalf("debia resolver la estatica (resueltos=%d)", pagos.resueltos)
	}
	if pagos.finalizados != 1 {
		t.Fatalf("debia finalizar el evento cancelado una vez (finalizados=%d)", pagos.finalizados)
	}

	// Otro tick: no vuelve a finalizar (idempotencia en memoria).
	must(t, w.Procesar(ctx))
	if pagos.finalizados != 1 {
		t.Fatalf("no debia finalizar dos veces (finalizados=%d)", pagos.finalizados)
	}
}

func TestTorneoCanceladoYSimulacionFallida(t *testing.T) {
	// Torneo cancelado y ademas la simulacion falla: igual hay que finalizar la
	// carrera (no se resuelve la estatica, pero el evento no puede quedar colgado).
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-6", CarreraID: "2024_monza", Estado: SimEnCurso},
		pilotos:    map[int]string{1: "VER", 4: "NOR"},
		posiciones: []EventoPosicion{{T: 100, Piloto: 1, Pos: 1}},
	}
	pagos := &pagosFalso{jugadores: nil}
	w := NuevoWorker(fuente, pagos, &torneoFalso{})
	ctx := context.Background()

	must(t, w.Procesar(ctx)) // en_curso: crea, abre, larga (cancelado)

	fuente.sim.Estado = SimFallida
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 0 {
		t.Fatalf("una simulacion fallida no resuelve la estatica (resueltos=%d)", pagos.resueltos)
	}
	if pagos.finalizados != 1 {
		t.Fatalf("un evento cancelado con carrera fallida debe finalizarse (finalizados=%d)", pagos.finalizados)
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
	// Torneo cancelado (sin jugadores): tambien finaliza la carrera en Pagos.
	if pagos.finalizados != 1 {
		t.Errorf("evento cancelado debia finalizarse una vez (finalizados=%d)", pagos.finalizados)
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

func TestSimulacionFallidaNoResuelve(t *testing.T) {
	// Una simulacion que se corta (fallida) arranco la carrera: el mercado se
	// cierra y las inscripciones tambien, pero NO se resuelve (no hay resultado
	// fiable). Resolver aqui pagaria a quien iba primero en el corte: incorrecto.
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-3", CarreraID: "2024_monza", Estado: SimEnCurso},
		pilotos:    map[int]string{1: "VER", 4: "NOR"},
		posiciones: []EventoPosicion{{T: 100, Piloto: 1, Pos: 1}},
	}
	pagos := &pagosFalso{jugadores: []string{"u1", "u2"}}
	w := NuevoWorker(fuente, pagos, &torneoFalso{})
	ctx := context.Background()

	// Tick 1: en_curso -> crea, abre, cierra y larga. No resuelve aun.
	must(t, w.Procesar(ctx))
	if pagos.cerrados != 1 || pagos.largadas != 1 {
		t.Fatalf("tras la largada: cerrados=%d largadas=%d, se esperaba 1 y 1", pagos.cerrados, pagos.largadas)
	}

	// Tick 2: la simulacion falla -> NO debe resolver.
	fuente.sim.Estado = SimFallida
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 0 {
		t.Fatalf("una simulacion fallida NO debe resolver (resueltos=%d)", pagos.resueltos)
	}

	// Tick 3: sigue fallida -> tampoco resuelve ni repite nada.
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 0 {
		t.Fatalf("un segundo tick fallida tampoco debe resolver (resueltos=%d)", pagos.resueltos)
	}
}

func TestSinGanadorReintentaYNoMarcaResuelto(t *testing.T) {
	// finalizada pero sin un lider claro (ningun position==1): resolver debe
	// fallar para que Procesar NO marque la fase como resuelta y lo reintente.
	// Cuando luego aparece el ganador, el siguiente tick resuelve.
	fuente := &fuenteFalsa{
		sim:        Simulacion{ID: "evt-4", CarreraID: "2024_monza", Estado: SimFinalizada},
		pilotos:    map[int]string{1: "VER", 4: "NOR"},
		posiciones: []EventoPosicion{{T: 100, Piloto: 1, Pos: 2}}, // nadie en P1
	}
	pagos := &pagosFalso{jugadores: nil}
	w := NuevoWorker(fuente, pagos, &torneoFalso{})
	ctx := context.Background()

	// Tick 1: se pone al dia (crea/larga) pero resolver falla -> error.
	if err := w.Procesar(ctx); err == nil {
		t.Fatal("sin ganador, Procesar debia devolver error para reintentar")
	}
	if pagos.resueltos != 0 {
		t.Fatalf("no debia resolver sin ganador (resueltos=%d)", pagos.resueltos)
	}

	// Tick 2: ahora llega el evento que pone a VER en P1 -> resuelve.
	fuente.posiciones = []EventoPosicion{{T: 200, Piloto: 1, Pos: 1}}
	must(t, w.Procesar(ctx))
	if pagos.resueltos != 1 || pagos.ultimaGanadora != "VER" {
		t.Fatalf("tras aparecer el ganador: resueltos=%d ganadora=%q, se esperaba 1 y VER", pagos.resueltos, pagos.ultimaGanadora)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
}
