// Package cierre conecta HU2-12: cierra el torneo de un evento cuando ya no
// queda nada por liquidar (RF-16).
//
//	Worker (cada Intervalo):
//	  1. eventos con torneo en Redis (fichas.Almacen.EventosActivos)
//	  2. GET /internal/events/{id}               race_key y tournament_status
//	  3. Mongo: ultima simulacion de race_key     tiene que estar "finalizada"
//	  4. GET /internal/events/{id}/pendientes    mercados sin liquidar y
//	                                             apuestas de torneo pending, en 0
//	  5. ranking_torneo final por WebSocket (antes de limpiar Redis)
//	  6. fichas.Almacen.CerrarTorneo             reparto en Pagos + limpia Redis
//
// Nunca antes de tiempo: si falta cualquiera de 3 o 4, espera a la siguiente
// vuelta. CerrarTorneo es idempotente (ver fichas/cierre.go); aun asi el
// worker recuerda en memoria los eventos ya cerrados para no llamarlo en cada
// vuelta. Tras un reinicio no hace falta: Redis ya se limpio y el evento no
// vuelve a aparecer en el paso 1.
//
// Sin Redis el worker no se arranca. Un torneo cancelado (<4 inscritos) no
// tiene fichas en Redis, asi que tampoco aparece.
//
// Dependencia: los mercados estaticos los liquida HU2-07. Mientras queden
// mercados del evento sin "liquidado", el torneo no se cierra.
package cierre

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// ErrPagosNoDisponible: Pagos no respondio o respondio algo inesperado.
var ErrPagosNoDisponible = errors.New("pagos no esta disponible")

// Valores de events.tournament_status y simulaciones.estado.
const (
	TorneoCancelado     = "cancelado"
	SimulacionTerminada = "finalizada"
)

// Evento es lo que importa de GET /internal/events/{id}.
type Evento struct {
	ID           string `json:"event_id"`
	RaceKey      string `json:"race_key"`
	EstadoTorneo string `json:"tournament_status"`
}

// Pendientes es la respuesta de GET /internal/events/{id}/pendientes.
type Pendientes struct {
	Mercados int `json:"mercados_sin_liquidar"`
	Torneo   int `json:"apuestas_torneo_pending"`
}

// Pagos es lo que el cierre necesita leer de Pagos.
type Pagos interface {
	Evento(ctx context.Context, eventoID string) (Evento, error)
	Pendientes(ctx context.Context, eventoID string) (Pendientes, error)
}

// Carrera da el estado de la ultima simulacion de una carrera (la fuente de
// HU2-14, adaptada en main.go). ok=false si nunca se simulo.
type Carrera interface {
	EstadoSimulacion(ctx context.Context, carreraID string) (estado string, ok bool, err error)
}

// Torneos es el almacen de fichas (HU2-11), adaptado en main.go.
type Torneos interface {
	EventosActivos(ctx context.Context) ([]string, error)
	// DifundirRanking envia ranking_torneo a todos.
	DifundirRanking(ctx context.Context, eventoID string)
	// Cerrar reporta las fichas finales a Pagos y limpia Redis (CerrarTorneo).
	Cerrar(ctx context.Context, eventoID string) error
}

type Worker struct {
	Torneos   Torneos
	Pagos     Pagos
	Carrera   Carrera
	Intervalo time.Duration

	cerrados map[string]bool // eventos ya cerrados en esta ejecucion
}

func NuevoWorker(t Torneos, p Pagos, c Carrera, intervalo time.Duration) *Worker {
	return &Worker{Torneos: t, Pagos: p, Carrera: c, Intervalo: intervalo}
}

// Run procesa cada Intervalo hasta que se cancele el contexto.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Intervalo)
	defer ticker.Stop()
	for {
		if err := w.Procesar(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-12: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Procesar revisa cada torneo activo y cierra los que ya terminaron. Un
// evento que falla no detiene a los demas; se devuelve el primer error.
func (w *Worker) Procesar(ctx context.Context) error {
	eventos, err := w.Torneos.EventosActivos(ctx)
	if err != nil {
		return fmt.Errorf("listar torneos activos: %w", err)
	}
	var primero error
	for _, e := range eventos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if w.cerrados[e] {
			continue
		}
		if err := w.CerrarSiTermino(ctx, e); err != nil && primero == nil {
			primero = fmt.Errorf("evento %s: %w", e, err)
		}
	}
	return primero
}

// CerrarSiTermino cierra el torneo del evento solo si la carrera termino y no
// queda nada por liquidar. Si no, no hace nada (se revisa en la siguiente vuelta).
func (w *Worker) CerrarSiTermino(ctx context.Context, eventoID string) error {
	ev, err := w.Pagos.Evento(ctx, eventoID)
	if err != nil {
		return fmt.Errorf("leer evento: %w", err)
	}
	if ev.EstadoTorneo == TorneoCancelado {
		return nil // no hay botin que repartir
	}
	estado, ok, err := w.Carrera.EstadoSimulacion(ctx, ev.RaceKey)
	if err != nil {
		return fmt.Errorf("leer simulacion de %s: %w", ev.RaceKey, err)
	}
	if !ok || estado != SimulacionTerminada {
		return nil // la carrera sigue (o aun no arranca)
	}
	p, err := w.Pagos.Pendientes(ctx, eventoID)
	if err != nil {
		return fmt.Errorf("leer pendientes: %w", err)
	}
	if p.Mercados > 0 || p.Torneo > 0 {
		return nil // espera a HU2-10 y HU2-07
	}

	// La foto final sale antes de cerrar: CerrarTorneo limpia Redis y despues
	// ya no hay fichas que mostrar.
	w.Torneos.DifundirRanking(ctx, eventoID)
	if err := w.Torneos.Cerrar(ctx, eventoID); err != nil {
		return fmt.Errorf("cerrar torneo: %w", err)
	}
	if w.cerrados == nil {
		w.cerrados = map[string]bool{}
	}
	w.cerrados[eventoID] = true
	log.Printf("HU2-12: torneo del evento %s cerrado y repartido", eventoID)
	return nil
}
