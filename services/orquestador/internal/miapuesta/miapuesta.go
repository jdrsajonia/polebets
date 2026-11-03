// Package miapuesta implementa la consulta de la apuesta del jugador en un
// mercado (HU2-13, seccion 3.7 de documentation/orquestador-api.md):
//
//	GET /mercados/{mercado_id}/mi-apuesta   (el Gateway la expone como /api/...)
//
// Solo jugadores. La usa el panel de dinamicas al recargar la pagina, para
// volver a mostrar lo que se aposto y, si ya se liquido, el resultado. Un
// paso: GET /internal/markets/{id}/bets?user_id= de Pagos, que devuelve a lo
// sumo una apuesta (uq_bets_user_market).
package miapuesta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

// Pagos es lo que este flujo necesita del cliente interno.
type Pagos interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// MiApuesta es el cuerpo publico. Status y Payout conservan los valores de
// Pagos: status pending | won | lost; payout es null mientras esta pending
// (en tokens o fichas segun el modo; 0 si perdio).
type MiApuesta struct {
	ApuestaID     string      `json:"apuesta_id"`
	MercadoID     string      `json:"mercado_id"`
	Opcion        string      `json:"opcion"`
	Monto         int64       `json:"monto"`
	Modo          string      `json:"modo"`
	Multiplicador json.Number `json:"multiplicador"`
	Status        string      `json:"status"`
	Payout        *int64      `json:"payout"`
}

// apuestaPagos es un elemento de GET /internal/markets/{id}/bets.
type apuestaPagos struct {
	BetID      string      `json:"bet_id"`
	Mode       string      `json:"mode"`
	Option     string      `json:"option"`
	Amount     int64       `json:"amount"`
	Multiplier json.Number `json:"multiplier"`
	Status     string      `json:"status"`
	Payout     *int64      `json:"payout"`
}

// Register agrega la ruta, protegida con el middleware de sesion.
func Register(mux *http.ServeMux, pagos Pagos) {
	mux.Handle("GET /mercados/{mercado_id}/mi-apuesta", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			userID, _ := sesion.UsuarioDe(r.Context())
			a, err := Ver(r.Context(), pagos, r.PathValue("mercado_id"), userID)
			if err != nil {
				errores.Responder(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.EscribirJSON(w, http.StatusOK, a)
		},
	)))
}

// Ver trae de Pagos la apuesta del jugador en el mercado.
func Ver(ctx context.Context, pagos Pagos, mercadoID, userID string) (MiApuesta, error) {
	resp, err := pagos.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodGet,
		Ruta:   "/internal/markets/" + url.PathEscape(mercadoID) + "/bets?user_id=" + url.QueryEscape(userID),
	})
	if err != nil {
		return MiApuesta{}, err // ErrNoDisponible -> 503; el resto -> 500
	}
	switch resp.Estado {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		// 422: el mercado_id no es un uuid; para el jugador es lo mismo.
		return MiApuesta{}, errores.MercadoNoExiste
	default:
		return MiApuesta{}, fmt.Errorf("pagos respondio %d al leer la apuesta: %s", resp.Estado, resp.Cuerpo)
	}

	var lista []apuestaPagos
	if err := json.Unmarshal(resp.Cuerpo, &lista); err != nil {
		return MiApuesta{}, fmt.Errorf("respuesta invalida de Pagos: %w", err)
	}
	if len(lista) == 0 {
		return MiApuesta{}, errores.SinApuesta
	}
	b := lista[0]
	return MiApuesta{
		ApuestaID:     b.BetID,
		MercadoID:     mercadoID,
		Opcion:        b.Option,
		Monto:         b.Amount,
		Modo:          b.Mode,
		Multiplicador: b.Multiplier,
		Status:        b.Status,
		Payout:        b.Payout,
	}, nil
}
