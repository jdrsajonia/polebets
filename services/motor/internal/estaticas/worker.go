// Package estaticas implementa HU2-07: el Gestor de mercados estaticos.
//
// Un worker del Motor vigila las simulaciones en MongoDB (que escribe el
// Ingestor; solo lectura, RNF-18) y orquesta el ciclo del evento y sus
// mercados estaticos en Pagos, por REST interno:
//
//	simulacion existe      -> crea el evento + abre el mercado "ganador"
//	simulacion largo        -> cierra el mercado + start (cierra inscripciones)
//	                           e inicializa las fichas del torneo (HU2-11)
//	simulacion finalizada   -> resuelve el mercado con el ganador real
//
// Todo lo decide el ESTADO de la simulacion, no la hora real. El ciclo es
// idempotente: reprocesar la misma simulacion no duplica nada porque Pagos es
// idempotente por event_id/market_id (derivados determinaisticamente) y las
// transiciones open/close/resolve/start lo son tambien.
//
// event_id = _id de la simulacion (acordado con HU2-08); race_key = carrera_id.
package estaticas

import (
	"context"
	"log"
	"time"
)

// Torneo es lo que el worker necesita para crear las fichas al largar (el
// almacen de HU2-11, adaptado en main.go). nil = sin torneo (sin Redis).
type Torneo interface {
	InicializarTorneo(ctx context.Context, eventoID string, jugadores []string, fichasIniciales int64) error
}

// faseSim lleva en memoria que ya se hizo de la simulacion actual. La verdad
// esta en Pagos (idempotente); esto solo evita repetir trabajo cada tick.
type faseSim struct {
	id       string
	abierto  bool // evento creado y mercado abierto
	largado  bool // mercado cerrado, inscripciones cerradas, fichas creadas
	resuelto bool // mercado resuelto
}

// Worker orquesta el ciclo de los mercados estaticos de la simulacion actual.
type Worker struct {
	Fuente    Fuente
	Pagos     Pagos
	Torneo    Torneo // puede ser nil
	Intervalo time.Duration

	fase faseSim
}

func NuevoWorker(f Fuente, p Pagos, t Torneo) *Worker {
	return &Worker{Fuente: f, Pagos: p, Torneo: t, Intervalo: time.Second}
}

// Run procesa cada Intervalo hasta que se cancele el contexto.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Intervalo)
	defer ticker.Stop()
	for {
		if err := w.Procesar(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-07: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Procesar hace un tick: lee la simulacion actual y avanza su ciclo lo que se
// pueda. Una fase que falla se reintenta en el siguiente tick (no avanza).
func (w *Worker) Procesar(ctx context.Context) error {
	sim, ok, err := w.Fuente.SimulacionActual(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if sim.ID != w.fase.id {
		w.fase = faseSim{id: sim.ID}
	}

	// Fase 1: evento creado y mercado abierto. Se hace apenas existe la
	// simulacion, en cualquier estado (si el Motor arranca con la carrera ya
	// en curso, igual crea y abre, y acto seguido las fases 2/3 lo ponen al dia).
	if !w.fase.abierto {
		if err := w.abrir(ctx, sim); err != nil {
			return err
		}
		w.fase.abierto = true
	}

	// Fase 2: la carrera largo -> cerrar estaticas y cerrar inscripciones.
	if sim.Largo() && !w.fase.largado {
		if err := w.largar(ctx, sim); err != nil {
			return err
		}
		w.fase.largado = true
	}

	// Fase 3: la carrera termino -> resolver el ganador.
	if sim.Termino() && !w.fase.resuelto {
		if err := w.resolver(ctx, sim); err != nil {
			return err
		}
		w.fase.resuelto = true
	}
	return nil
}

// abrir crea el evento en Pagos y abre el mercado "ganador" con las opciones
// de los pilotos de la carrera.
func (w *Worker) abrir(ctx context.Context, sim Simulacion) error {
	nuevo, err := w.Pagos.CrearEvento(ctx, AltaEvento{
		EventoID:    sim.ID,
		RaceKey:     sim.CarreraID,
		HoraLargada: sim.HoraLargada,
	})
	if err != nil {
		return err
	}
	if nuevo {
		log.Printf("HU2-07: evento %s creado (carrera %s)", sim.ID, sim.CarreraID)
	}

	datos, err := w.Fuente.Carrera(ctx, sim.CarreraID)
	if err != nil {
		return err
	}
	opciones := opcionesGanador(datos.Pilotos)
	if len(opciones) < 2 {
		// Un mercado necesita al menos 2 opciones; sin pilotos no se abre.
		log.Printf("HU2-07: ATENCION la carrera %s tiene %d pilotos con codigo; no se abre el mercado ganador", sim.CarreraID, len(opciones))
		return nil
	}

	// Si la carrera ya largo, el mercado nace cerrado: nadie apuesta tarde.
	estado := EstadoAbierto
	if sim.Largo() {
		estado = EstadoProgramado
	}
	nuevoM, err := w.Pagos.CrearMercado(ctx, AltaMercado{
		MercadoID: IDMercado(sim.ID, PlantillaGanador),
		EventoID:  sim.ID,
		Plantilla: PlantillaGanador,
		Pregunta:  PreguntaGanador,
		Opciones:  opciones,
		Estado:    estado,
	})
	if err != nil {
		return err
	}
	if nuevoM {
		log.Printf("HU2-07: mercado ganador abierto para el evento %s (%d opciones, estado=%s)", sim.ID, len(opciones), estado)
	}
	return nil
}

// largar cierra el mercado estatico y las inscripciones del torneo, y crea las
// fichas de los jugadores inscritos (HU2-11).
func (w *Worker) largar(ctx context.Context, sim Simulacion) error {
	if err := w.Pagos.Cerrar(ctx, IDMercado(sim.ID, PlantillaGanador)); err != nil {
		return err
	}
	largada, err := w.Pagos.Largar(ctx, sim.ID)
	if err != nil {
		return err
	}
	log.Printf("HU2-07: evento %s largado: mercado estatico cerrado, %d inscritos al torneo", sim.ID, len(largada.Jugadores))

	if w.Torneo != nil && len(largada.Jugadores) > 0 {
		if err := w.Torneo.InicializarTorneo(ctx, sim.ID, largada.Jugadores, largada.FichasIniciales); err != nil {
			return err
		}
		log.Printf("HU2-07: fichas del torneo inicializadas para %d jugadores (evento %s)", len(largada.Jugadores), sim.ID)
	}
	return nil
}

// resolver lee las posiciones reales de la carrera, calcula el ganador y lo
// escribe en el mercado. HU2-10 paga las apuestas; este worker no paga.
func (w *Worker) resolver(ctx context.Context, sim Simulacion) error {
	posiciones, err := w.Fuente.Posiciones(ctx, sim.CarreraID)
	if err != nil {
		return err
	}
	datos, err := w.Fuente.Carrera(ctx, sim.CarreraID)
	if err != nil {
		return err
	}
	codigo, ok := ganador(posiciones, datos.Pilotos)
	if !ok {
		log.Printf("HU2-07: ATENCION no se pudo determinar el ganador de la carrera %s; el mercado estatico queda sin resolver", sim.CarreraID)
		return nil
	}
	if err := w.Pagos.Resolver(ctx, IDMercado(sim.ID, PlantillaGanador), codigo); err != nil {
		return err
	}
	log.Printf("HU2-07: mercado ganador del evento %s resuelto: gana %q", sim.ID, codigo)
	return nil
}
