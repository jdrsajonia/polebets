// Package apuesta implementa el flujo de apuesta (HU2-09, seccion 3.6 de
// documentation/orquestador-api.md):
//
//	POST /apuestas   (el Gateway la expone como /api/apuestas)
//
// Solo jugadores. Idempotency-Key obligatoria: la genera el front una vez
// por apuesta y la reutiliza en los reintentos. Un paso: el Motor decide el
// modo (individual o torneo), valida el mercado y habla con Pagos. El
// Orquestador solo valida la forma del cuerpo, reenvia y traduce errores.
package apuesta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

// LargoMaximoClave es el largo maximo de la Idempotency-Key del front.
const LargoMaximoClave = 128

// Motor es lo que este flujo necesita del cliente interno.
type Motor interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// Peticion es el cuerpo publico.
type Peticion struct {
	MercadoID string `json:"mercado_id"`
	Opcion    string `json:"opcion"`
	Monto     int64  `json:"monto"`
}

// Respuesta es el cuerpo publico (201 nueva, 200 reenvio con la misma clave).
type Respuesta struct {
	ApuestaID     string      `json:"apuesta_id"`
	MercadoID     string      `json:"mercado_id"`
	Opcion        string      `json:"opcion"`
	Monto         int64       `json:"monto"`
	Modo          string      `json:"modo"`
	Multiplicador json.Number `json:"multiplicador"`
	PagoSiAcierta int64       `json:"pago_si_acierta"`
	Saldo         *int64      `json:"saldo"`
	Fichas        *int64      `json:"fichas"`
}

// codigos del Motor que pasan tal cual a la tabla publica.
var traduccion = map[string]errores.Error{
	"mercado_no_existe":    errores.MercadoNoExiste,
	"mercado_cerrado":      errores.MercadoCerrado,
	"ya_apostaste":         errores.YaApostaste,
	"clave_reutilizada":    errores.ClaveReutilizada,
	"monto_invalido":       errores.MontoInvalido,
	"opcion_invalida":      errores.OpcionInvalida,
	"saldo_insuficiente":   errores.SaldoInsuficiente,
	"fichas_insuficientes": errores.FichasInsuficientes,
	"cuerpo_invalido":      errores.CuerpoInvalido,
}

// Register agrega la ruta, protegida con el middleware de sesion.
func Register(mux *http.ServeMux, motor Motor) {
	mux.Handle("POST /apuestas", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			clave := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if clave == "" || len(clave) > LargoMaximoClave {
				errores.Responder(w, errores.FaltaIdempotencyKey)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			var p Peticion
			if err := dec.Decode(&p); err != nil || strings.TrimSpace(p.MercadoID) == "" || strings.TrimSpace(p.Opcion) == "" {
				errores.Responder(w, errores.CuerpoInvalido)
				return
			}
			if p.Monto <= 0 {
				errores.Responder(w, errores.MontoInvalido)
				return
			}
			userID, _ := sesion.UsuarioDe(r.Context())
			resp, estado, err := Apostar(r.Context(), motor, userID, p, clave)
			if err != nil {
				errores.Responder(w, err)
				return
			}
			httpx.EscribirJSON(w, estado, resp)
		},
	)))
}

// Apostar reenvia al Motor y traduce su respuesta.
func Apostar(ctx context.Context, motor Motor, userID string, p Peticion, clave string) (Respuesta, int, error) {
	resp, err := motor.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodPost,
		Ruta:   "/internal/apuestas",
		Cuerpo: map[string]any{
			"user_id":    userID,
			"mercado_id": strings.TrimSpace(p.MercadoID),
			"opcion":     strings.TrimSpace(p.Opcion),
			"monto":      p.Monto,
		},
		// La clave del front tal cual; el Motor la combina con el user_id.
		IdempotencyKey: clave,
	})
	if err != nil {
		return Respuesta{}, 0, err // ErrNoDisponible -> 503
	}

	switch resp.Estado {
	case http.StatusCreated, http.StatusOK:
		var r Respuesta
		if err := json.Unmarshal(resp.Cuerpo, &r); err != nil {
			return Respuesta{}, 0, fmt.Errorf("respuesta invalida del Motor: %w", err)
		}
		return r, resp.Estado, nil
	default:
		var e struct {
			Codigo string `json:"codigo"`
		}
		_ = json.Unmarshal(resp.Cuerpo, &e)
		if publico, ok := traduccion[e.Codigo]; ok {
			return Respuesta{}, 0, publico
		}
		if e.Codigo == "servicio_no_disponible" {
			return Respuesta{}, 0, fmt.Errorf("%w: el Motor no alcanzo a Pagos", interno.ErrNoDisponible)
		}
		return Respuesta{}, 0, errors.New("el Motor respondio " + http.StatusText(resp.Estado) + ": " + string(resp.Cuerpo))
	}
}
