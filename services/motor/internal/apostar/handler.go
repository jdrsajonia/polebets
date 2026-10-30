package apostar

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/arquisoft/motor/internal/httpx"
)

// Register agrega POST /internal/apuestas. Solo la llama el Orquestador, con
// el token de servicio; el jugador viene en el cuerpo (user_id), que el
// Orquestador toma de los headers del Gateway.
//
// Errores: {"error": mensaje, "codigo": codigo}, con los codigos de la tabla
// de la seccion 2 de documentation/orquestador-api.md.
func Register(mux *http.ServeMux, svc *Servicio, token string) {
	mux.HandleFunc("POST /internal/apuestas", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.Header.Get("X-Internal-Service-Token")), []byte(token)) {
			responderError(w, http.StatusUnauthorized, "servicio_no_autorizado", "Servicio no autorizado")
			return
		}
		clave := r.Header.Get("Idempotency-Key")
		if clave == "" || len(clave) > 128 {
			responderError(w, http.StatusBadRequest, "falta_idempotency_key", "Falta el header Idempotency-Key o es inválido")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var p Peticion
		if err := dec.Decode(&p); err != nil {
			responderError(w, http.StatusBadRequest, "cuerpo_invalido", "Revisa los datos enviados")
			return
		}

		apuesta, err := svc.Apostar(r.Context(), p, clave)
		if err != nil {
			estado, codigo, msg := traducir(err)
			responderError(w, estado, codigo, msg)
			return
		}
		estado := http.StatusOK
		if apuesta.Nueva {
			estado = http.StatusCreated
		}
		httpx.EscribirJSON(w, estado, apuesta)
	})
}

func traducir(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrPeticionInvalida):
		return http.StatusBadRequest, "cuerpo_invalido", "Revisa los datos enviados"
	case errors.Is(err, ErrMercadoNoExiste):
		return http.StatusNotFound, "mercado_no_existe", "El mercado no existe"
	case errors.Is(err, ErrMercadoCerrado):
		return http.StatusConflict, "mercado_cerrado", "Mercado cerrado"
	case errors.Is(err, ErrYaApostaste):
		return http.StatusConflict, "ya_apostaste", "Ya apostaste en este mercado"
	case errors.Is(err, ErrClaveReutilizada):
		return http.StatusConflict, "clave_reutilizada", "Esta solicitud ya se usó con otros datos"
	case errors.Is(err, ErrMontoInvalido):
		return http.StatusUnprocessableEntity, "monto_invalido", "Monto inválido"
	case errors.Is(err, ErrOpcionInvalida):
		return http.StatusUnprocessableEntity, "opcion_invalida", "La opción no pertenece al mercado"
	case errors.Is(err, ErrSaldoInsuficiente):
		return http.StatusUnprocessableEntity, "saldo_insuficiente", "Saldo insuficiente"
	case errors.Is(err, ErrPagosNoDisponible):
		log.Printf("HU2-09: %v", err)
		return http.StatusServiceUnavailable, "servicio_no_disponible", "El servicio no está disponible, intenta de nuevo"
	default:
		log.Printf("HU2-09: error inesperado al apostar: %v", err)
		return http.StatusInternalServerError, "error_interno", "Error interno"
	}
}

func responderError(w http.ResponseWriter, estado int, codigo, msg string) {
	httpx.EscribirJSON(w, estado, map[string]string{"error": msg, "codigo": codigo})
}
