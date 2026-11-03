// Package liquidar implementa HU2-10: liquidacion de los mercados del
// Prototipo 2 (los que viven en Pagos, tabla markets), en los dos modos.
//
//	Pagos: mercado "cerrado" con winning_option
//	   │  (lo escribe quien conoce el resultado: HU2-07/08 con el Ingestor)
//	   ▼
//	Worker (cada Intervalo):
//	  1. GET  /internal/markets?status=cerrado&resuelto=true
//	  2. GET  /internal/markets/{id}/bets?mode=individual&status=pending
//	     POST /internal/settlements por apuesta (won si opcion == ganadora)
//	     aviso "resultado_apuesta" (tokens) al jugador por WebSocket
//	  3. GET  /internal/markets/{id}/bets?mode=torneo&status=pending
//	     fichas en Redis (fichas.LiquidarYUbicar, HU2-11)
//	     POST /internal/bets/torneo/{id}/settlement (marca won/lost en Pagos)
//	     aviso "resultado_apuesta" (fichas y puesto) al jugador
//	     al final, "ranking_torneo" a todos
//	  4. POST /internal/markets/{id}/liquidated, solo sin pendientes
//
// Idempotencia (RNF-12): la clave de cada pago individual es
// bet-settlement:{bet_id}. En torneo, Redis escribe el resultado una sola vez
// por apuesta y recalcula el total (las fichas no se suman), y Pagos solo
// marca la apuesta. Si el Motor se cae a mitad de un mercado, la siguiente
// vuelta repite y nada se paga dos veces. Las apuestas ya marcadas no vuelven
// a aparecer. Un mercado con algo fallido no se marca liquidado: se reintenta.
//
// Sin Redis (Fichas nil) las de torneo quedan pendientes y el mercado espera.
// Torneo cancelado (<4 inscritos): no hay fichas y el Motor apuesta todo en
// individual, asi que el paso 3 no encuentra nada.
//
// Este worker no reemplaza al del P1 (paquete liquidacion), que sigue
// leyendo los mercados de Mongo.
package liquidar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// ErrPagosNoDisponible: Pagos no respondio o respondio algo inesperado.
var ErrPagosNoDisponible = errors.New("pagos no esta disponible")

// TipoResultadoApuesta es el mensaje que el Front ya escucha (HU-08).
const TipoResultadoApuesta = "resultado_apuesta"

// Mercado es lo que importa de GET /internal/markets.
type Mercado struct {
	ID       string  `json:"market_id"`
	EventoID string  `json:"event_id"`
	Pregunta string  `json:"question"`
	Estado   string  `json:"status"`
	Ganadora *string `json:"winning_option"`
}

// Apuesta es una fila de GET /internal/markets/{id}/bets.
type Apuesta struct {
	ID            string  `json:"bet_id"`
	UserID        string  `json:"user_id"`
	Modo          string  `json:"mode"`
	Opcion        string  `json:"option"`
	Monto         int64   `json:"amount"`
	Multiplicador float64 `json:"multiplier"`
	Estado        string  `json:"status"`
}

// Pago es la respuesta de POST /internal/settlements.
type Pago struct {
	Estado string `json:"status"` // won | lost
	Pago   int64  `json:"payout"` // tokens recibidos (0 si perdio)
	Saldo  int64  `json:"saldo"`
	Nueva  bool   `json:"nueva"`
}

// Resultado es el payload de "resultado_apuesta" para una apuesta individual.
type Resultado struct {
	MercadoID     string  `json:"mercado_id"`
	ApuestaID     string  `json:"apuesta_id"`
	Pregunta      string  `json:"pregunta"`
	Opcion        string  `json:"opcion"`
	Ganadora      string  `json:"opcion_ganadora"`
	Gano          bool    `json:"gano"`
	Modo          string  `json:"modo"` // siempre "individual"
	Monto         int64   `json:"monto"`
	Multiplicador float64 `json:"multiplicador"`
	Pago          int64   `json:"pago"`  // tokens recibidos
	Saldo         int64   `json:"saldo"` // saldo despues de liquidar
}

// PagoTorneo es la respuesta de POST /internal/bets/torneo/{id}/settlement.
type PagoTorneo struct {
	Estado string `json:"status"` // won | lost
	Pago   int64  `json:"payout"` // fichas (0 si perdio)
	Nueva  bool   `json:"nueva"`
}

// ResultadoTorneo es el payload de "resultado_apuesta" para una apuesta de
// torneo (RF-17): fichas en vez de tokens, y el puesto en el ranking.
type ResultadoTorneo struct {
	MercadoID      string  `json:"mercado_id"`
	ApuestaID      string  `json:"apuesta_id"`
	Pregunta       string  `json:"pregunta"`
	Opcion         string  `json:"opcion"`
	Ganadora       string  `json:"opcion_ganadora"`
	Gano           bool    `json:"gano"`
	Modo           string  `json:"modo"` // siempre "torneo"
	Monto          int64   `json:"monto"`
	Multiplicador  float64 `json:"multiplicador"`
	FichasGanadas  int64   `json:"fichas_ganadas"`  // 0 si perdio
	FichasPerdidas int64   `json:"fichas_perdidas"` // el monto si perdio, 0 si gano
	FichasTotal    int64   `json:"fichas_total"`
	Puesto         int     `json:"puesto"`
}

// Pagos es lo que el worker necesita del servicio de Pagos.
type Pagos interface {
	MercadosPorLiquidar(ctx context.Context) ([]Mercado, error)
	// ApuestasPendientes lista las pending del mercado en un modo
	// ("individual" o "torneo").
	ApuestasPendientes(ctx context.Context, mercadoID, modo string) ([]Apuesta, error)
	Liquidar(ctx context.Context, betID string, gano bool) (Pago, error)
	LiquidarTorneo(ctx context.Context, betID string, gano bool) (PagoTorneo, error)
	MarcarLiquidado(ctx context.Context, mercadoID string) error
}

// ErrSinReserva: Redis respondio bien pero la apuesta no tiene reserva de
// fichas (el registro en Pagos se hizo y la reserva fallo, y el jugador no
// reintento; ver apostar.apostarTorneo). No se le descontaron fichas.
var ErrSinReserva = errors.New("la apuesta de torneo no tiene reserva de fichas")

// FichasTorneo es el resultado en fichas de liquidar una apuesta de torneo.
type FichasTorneo struct {
	Ganadas int64
	Total   int64
	Puesto  int
}

// Fichas es lo que el worker necesita del almacen del torneo (HU2-11),
// adaptado en main.go. nil = Motor sin Redis.
type Fichas interface {
	// LiquidarApuesta escribe el resultado en Redis (idempotente: repetir no
	// cambia las fichas). ErrSinReserva si la reserva no existe; cualquier
	// otro error es de conexion y se reintenta.
	LiquidarApuesta(ctx context.Context, eventoID, apuestaID string, gano bool, multiplicador float64) (FichasTorneo, error)
	// Difundir envia "ranking_torneo" a todos.
	Difundir(ctx context.Context, eventoID string)
}

// Avisador manda un mensaje a un jugador (el Hub del WebSocket, adaptado en
// main.go). nil = no se avisa.
type Avisador interface {
	EnviarA(userID, tipo string, payload any) error
}

type Worker struct {
	Pagos     Pagos
	Fichas    Fichas
	Avisador  Avisador
	Intervalo time.Duration
}

func NuevoWorker(p Pagos, a Avisador, intervalo time.Duration) *Worker {
	return &Worker{Pagos: p, Avisador: a, Intervalo: intervalo}
}

// Run procesa cada Intervalo hasta que se cancele el contexto.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Intervalo)
	defer ticker.Stop()
	for {
		if err := w.Procesar(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-10: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Procesar hace una vuelta: liquida todos los mercados listos. Un mercado que
// falla no detiene a los demas; se devuelve el primer error para el log.
func (w *Worker) Procesar(ctx context.Context) error {
	mercados, err := w.Pagos.MercadosPorLiquidar(ctx)
	if err != nil {
		return fmt.Errorf("listar mercados por liquidar: %w", err)
	}
	var primero error
	for _, m := range mercados {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := w.LiquidarMercado(ctx, m); err != nil {
			log.Printf("HU2-10: mercado %s queda para la siguiente vuelta: %v", m.ID, err)
			if primero == nil {
				primero = err
			}
		}
	}
	return primero
}

// LiquidarMercado paga las apuestas pendientes del mercado (individuales y de
// torneo) y lo marca liquidado. Si algo falla o queda alguna de torneo sin
// poder liquidar, no lo marca (se reintenta luego).
func (w *Worker) LiquidarMercado(ctx context.Context, m Mercado) error {
	if m.Ganadora == nil || *m.Ganadora == "" {
		return nil // aun sin resultado: no es de este worker
	}
	ganadora := *m.Ganadora
	// Un modo que falla no frena al otro; el mercado solo se marca si los dos
	// quedaron sin pendientes.
	individuales, errInd := w.liquidarIndividuales(ctx, m, ganadora)
	torneo, errTor := w.liquidarTorneo(ctx, m, ganadora)
	if err := errors.Join(errInd, errTor); err != nil {
		return err
	}
	if err := w.Pagos.MarcarLiquidado(ctx, m.ID); err != nil {
		return fmt.Errorf("marcar liquidado: %w", err)
	}
	log.Printf("HU2-10: mercado %s liquidado (%d individuales, %d de torneo, gano %q)", m.ID, individuales, torneo, ganadora)
	return nil
}

func (w *Worker) liquidarIndividuales(ctx context.Context, m Mercado, ganadora string) (int, error) {
	apuestas, err := w.Pagos.ApuestasPendientes(ctx, m.ID, "individual")
	if err != nil {
		return 0, fmt.Errorf("leer apuestas individuales: %w", err)
	}
	var fallo error
	for _, a := range apuestas {
		gano := a.Opcion == ganadora
		p, err := w.Pagos.Liquidar(ctx, a.ID, gano)
		if err != nil {
			fallo = fmt.Errorf("liquidar apuesta %s: %w", a.ID, err)
			continue
		}
		if p.Nueva {
			w.avisar(a.UserID, Resultado{
				MercadoID: m.ID, ApuestaID: a.ID, Pregunta: m.Pregunta,
				Opcion: a.Opcion, Ganadora: ganadora, Gano: gano, Modo: "individual",
				Monto: a.Monto, Multiplicador: a.Multiplicador, Pago: p.Pago, Saldo: p.Saldo,
			})
		}
	}
	return len(apuestas), fallo
}

// liquidarTorneo: por cada apuesta, primero las fichas en Redis y despues la
// marca en Pagos. En ese orden, si Pagos falla la apuesta sigue pending y la
// siguiente vuelta repite Redis (no cambia nada) y vuelve a intentar Pagos.
func (w *Worker) liquidarTorneo(ctx context.Context, m Mercado, ganadora string) (int, error) {
	apuestas, err := w.Pagos.ApuestasPendientes(ctx, m.ID, "torneo")
	if err != nil {
		return 0, fmt.Errorf("leer apuestas de torneo: %w", err)
	}
	if len(apuestas) == 0 {
		return 0, nil
	}
	if w.Fichas == nil {
		return 0, fmt.Errorf("%d apuestas de torneo esperan Redis (Motor sin REDIS_URL)", len(apuestas))
	}
	var fallo error
	for _, a := range apuestas {
		gano := a.Opcion == ganadora
		f, err := w.Fichas.LiquidarApuesta(ctx, m.EventoID, a.ID, gano, a.Multiplicador)
		sinReserva := errors.Is(err, ErrSinReserva)
		switch {
		case sinReserva:
			// Redis respondio y la reserva no existe: nunca se descontaron
			// fichas. Se cierra como perdida con 0 fichas para no bloquear el
			// mercado; el total del jugador no cambia.
			log.Printf("HU2-10: apuesta de torneo %s (user_id=%s, mercado %s) sin reserva de fichas: se marca lost con 0 fichas", a.ID, a.UserID, m.ID)
			gano = false
		case err != nil:
			fallo = fmt.Errorf("fichas de la apuesta %s: %w", a.ID, err)
			continue
		}
		p, err := w.Pagos.LiquidarTorneo(ctx, a.ID, gano)
		if err != nil {
			fallo = fmt.Errorf("marcar apuesta de torneo %s: %w", a.ID, err)
			continue
		}
		if p.Nueva && !sinReserva {
			r := ResultadoTorneo{
				MercadoID: m.ID, ApuestaID: a.ID, Pregunta: m.Pregunta,
				Opcion: a.Opcion, Ganadora: ganadora, Gano: gano, Modo: "torneo",
				Monto: a.Monto, Multiplicador: a.Multiplicador,
				FichasGanadas: f.Ganadas, FichasTotal: f.Total, Puesto: f.Puesto,
			}
			if !gano {
				r.FichasPerdidas = a.Monto
			}
			w.avisar(a.UserID, r)
		}
	}
	// El ranking cambio aunque alguna apuesta haya fallado: se difunde igual.
	w.Fichas.Difundir(ctx, m.EventoID)
	return len(apuestas), fallo
}

// avisar no falla la liquidacion: el pago ya quedo hecho en Pagos.
func (w *Worker) avisar(userID string, r any) {
	if w.Avisador == nil {
		return
	}
	if err := w.Avisador.EnviarA(userID, TipoResultadoApuesta, r); err != nil {
		log.Printf("HU2-10: no se pudo avisar a user_id=%s: %v", userID, err)
	}
}
