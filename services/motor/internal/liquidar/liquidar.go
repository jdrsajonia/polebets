// Package liquidar implementa HU2-10: liquidacion individual de los mercados
// del Prototipo 2 (los que viven en Pagos, tabla markets).
//
//	Pagos: mercado "cerrado" con winning_option
//	   │  (lo escribe quien conoce el resultado: HU2-07/08 con el Ingestor)
//	   ▼
//	Worker (cada Intervalo):
//	  1. GET  /internal/markets?status=cerrado&resuelto=true
//	  2. GET  /internal/markets/{id}/bets?mode=individual&status=pending
//	  3. POST /internal/settlements por apuesta (won si opcion == ganadora)
//	  4. aviso "resultado_apuesta" al jugador por WebSocket
//	  5. POST /internal/markets/{id}/liquidated
//
// Idempotencia (RNF-12): la clave de cada pago es bet-settlement:{bet_id};
// si el Motor se cae a mitad de un mercado, la siguiente vuelta repite y
// Pagos no paga dos veces. Las apuestas ya pagadas no vuelven a aparecer en
// el paso 2. Un mercado con un pago fallido no se marca liquidado: se
// reintenta en la siguiente vuelta.
//
// Las apuestas de torneo (fichas) no se tocan aqui: son de HU2-11.
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

// Pagos es lo que el worker necesita del servicio de Pagos.
type Pagos interface {
	MercadosPorLiquidar(ctx context.Context) ([]Mercado, error)
	ApuestasPendientes(ctx context.Context, mercadoID string) ([]Apuesta, error)
	Liquidar(ctx context.Context, betID string, gano bool) (Pago, error)
	MarcarLiquidado(ctx context.Context, mercadoID string) error
}

// Avisador manda un mensaje a un jugador (el Hub del WebSocket, adaptado en
// main.go). nil = no se avisa.
type Avisador interface {
	EnviarA(userID, tipo string, payload any) error
}

type Worker struct {
	Pagos     Pagos
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

// LiquidarMercado paga las apuestas individuales pendientes del mercado y lo
// marca liquidado. Si algun pago falla, no lo marca (se reintenta luego).
func (w *Worker) LiquidarMercado(ctx context.Context, m Mercado) error {
	if m.Ganadora == nil || *m.Ganadora == "" {
		return nil // aun sin resultado: no es de este worker
	}
	ganadora := *m.Ganadora
	apuestas, err := w.Pagos.ApuestasPendientes(ctx, m.ID)
	if err != nil {
		return fmt.Errorf("leer apuestas: %w", err)
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
	if fallo != nil {
		return fallo
	}
	if err := w.Pagos.MarcarLiquidado(ctx, m.ID); err != nil {
		return fmt.Errorf("marcar liquidado: %w", err)
	}
	log.Printf("HU2-10: mercado %s liquidado (%d apuestas individuales, gano %q)", m.ID, len(apuestas), ganadora)
	return nil
}

// avisar no falla la liquidacion: el pago ya quedo hecho en Pagos.
func (w *Worker) avisar(userID string, r Resultado) {
	if w.Avisador == nil {
		return
	}
	if err := w.Avisador.EnviarA(userID, TipoResultadoApuesta, r); err != nil {
		log.Printf("HU2-10: no se pudo avisar a user_id=%s: %v", userID, err)
	}
}
