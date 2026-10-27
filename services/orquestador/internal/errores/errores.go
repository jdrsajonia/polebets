// Package errores define el formato de error comun de la API publica del
// orquestador (seccion 2 de documentation/orquestador-api.md):
//
//	{ "error": "Mercado cerrado", "codigo": "mercado_cerrado" }
//
// "error" es el mensaje que el front muestra tal cual (services/front/src/api/http.ts
// ya lo lee) y "codigo" es un identificador estable para que el front decida
// que hacer sin comparar textos.
package errores

import (
	"errors"
	"log"
	"net/http"

	"github.com/arquisoft/orquestador/internal/httpx"
)

// Error es un error de la API publica: codigo HTTP, codigo estable y mensaje.
//
// Es un valor comparable, asi que errors.Is(err, errores.MercadoCerrado)
// funciona aunque el error venga envuelto con fmt.Errorf("...: %w", ...).
type Error struct {
	Estado  int
	Codigo  string
	Mensaje string
}

func (e Error) Error() string { return e.Codigo + ": " + e.Mensaje }

// Tabla de errores de la seccion 2 del contrato. Todos los flujos usan estos
// valores; si hace falta uno nuevo, se agrega aqui y en el contrato.
var (
	CuerpoInvalido        = Error{http.StatusBadRequest, "cuerpo_invalido", "Revisa los datos enviados"}
	FaltaIdempotencyKey   = Error{http.StatusBadRequest, "falta_idempotency_key", "Falta el header Idempotency-Key o es inválido"}
	SinSesion             = Error{http.StatusUnauthorized, "sin_sesion", "No hay sesión"}
	SoloJugadores         = Error{http.StatusForbidden, "solo_jugadores", "Solo los jugadores"}
	EventoNoExiste        = Error{http.StatusNotFound, "evento_no_existe", "El evento no existe"}
	MercadoNoExiste       = Error{http.StatusNotFound, "mercado_no_existe", "El mercado no existe"}
	CorreoRegistrado      = Error{http.StatusConflict, "correo_registrado", "Este correo ya está registrado"}
	InscripcionesCerradas = Error{http.StatusConflict, "inscripciones_cerradas", "La carrera ya empezó: las inscripciones están cerradas"}
	MercadoCerrado        = Error{http.StatusConflict, "mercado_cerrado", "Mercado cerrado"}
	YaApostaste           = Error{http.StatusConflict, "ya_apostaste", "Ya apostaste en este mercado"}
	ClaveReutilizada      = Error{http.StatusConflict, "clave_reutilizada", "Esta solicitud ya se usó con otros datos"}
	SaldoInsuficiente     = Error{http.StatusUnprocessableEntity, "saldo_insuficiente", "Saldo insuficiente"}
	FichasInsuficientes   = Error{http.StatusUnprocessableEntity, "fichas_insuficientes", "Fichas insuficientes"}
	MontoInvalido         = Error{http.StatusUnprocessableEntity, "monto_invalido", "Monto inválido"}
	OpcionInvalida        = Error{http.StatusUnprocessableEntity, "opcion_invalida", "La opción no pertenece al mercado"}
	ServicioNoDisponible  = Error{http.StatusServiceUnavailable, "servicio_no_disponible", "El servicio no está disponible, intenta de nuevo"}
	Interno               = Error{http.StatusInternalServerError, "error_interno", "Error interno"}
)

// Cuerpo es el JSON de error que recibe el front.
type Cuerpo struct {
	Error  string `json:"error"`
	Codigo string `json:"codigo"`
}

// Responder escribe err con el formato comun. Si err no es un Error de la
// tabla (un fallo no previsto), responde 500 error_interno y deja el detalle
// solo en el log: al jugador nunca le llega un mensaje interno.
func Responder(w http.ResponseWriter, err error) {
	var e Error
	if !errors.As(err, &e) {
		log.Printf("error inesperado: %v", err)
		e = Interno
	}
	httpx.EscribirJSON(w, e.Estado, Cuerpo{Error: e.Mensaje, Codigo: e.Codigo})
}
