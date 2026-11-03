// Package posiciones sirve la foto inicial de la tabla de posiciones al Front.
//
//	GET /posiciones   (el Gateway la expone como /api/posiciones)
//
// El Front la pide al cargar o recargar /carrera, para pintar la tabla sin
// esperar al primer mensaje del WebSocket. El Orquestador la lee del Motor por
// REST interno (GET /internal/posiciones, token de servicio) y la reenvia tal
// cual: la foto es un JSON opaco para el Orquestador (no la interpreta).
//
// Solo jugadores. Es una lectura: el cliente interno ya reintenta; si el Motor
// no responde tras los reintentos -> 503. Si no hay carrera en curso, el Motor
// responde 404 y se reenvia ese 404 (el Front lo lee como "sin carrera").
package posiciones

import (
	"context"
	"net/http"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

// Servicio es lo que este flujo necesita de un cliente interno.
type Servicio interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// Register agrega GET /posiciones, protegida por sesion de jugador.
func Register(mux *http.ServeMux, motor Servicio) {
	mux.Handle("GET /posiciones", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			estado, cuerpo, err := Foto(r.Context(), motor)
			if err != nil {
				errores.Responder(w, err) // Motor caido -> 503
				return
			}
			// La foto cambia en vivo: nunca se cachea.
			w.Header().Set("Cache-Control", "no-store")
			// Se reenvia el cuerpo del Motor tal cual (200 con la foto, o 404).
			httpx.EscribirCrudo(w, estado, "application/json", cuerpo)
		},
	)))
}

// Foto lee la ultima foto del Motor (GET /internal/posiciones) y devuelve el
// estado y el cuerpo tal cual, para reenviarlos. Solo 200 y 404 son validos;
// cualquier otro estado se trata como Motor no disponible (503).
func Foto(ctx context.Context, motor Servicio) (int, []byte, error) {
	resp, err := motor.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodGet,
		Ruta:   "/internal/posiciones",
	})
	if err != nil {
		return 0, nil, err // ErrNoDisponible -> 503
	}
	if resp.Estado != http.StatusOK && resp.Estado != http.StatusNotFound {
		return 0, nil, interno.ErrNoDisponible
	}
	return resp.Estado, resp.Cuerpo, nil
}
