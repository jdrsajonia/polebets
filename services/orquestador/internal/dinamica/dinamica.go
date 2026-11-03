// Package dinamica sirve al Front la foto inicial del mercado dinamico.
//
//	GET /dinamica-actual   (el Gateway la expone como /api/dinamica-actual)
//
// El Front la pide al cargar o recargar /carrera, para pintar la apuesta en
// vivo sin esperar al reenvio periodico del WebSocket (~5 s). El Orquestador
// la lee del Motor por REST interno (GET /internal/dinamica-actual, token de
// servicio) y la reenvia tal cual: el mensaje es un JSON opaco para el
// Orquestador (no lo interpreta).
//
// Solo jugadores. Es una lectura: el cliente interno ya reintenta; si el Motor
// no responde tras los reintentos -> 503. Si no hay mercado dinamico vigente,
// el Motor responde 404 y se reenvia ese 404 (el Front lo lee como "sin
// apuesta en vivo ahora").
package dinamica

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

// Register agrega GET /dinamica-actual, protegida por sesion de jugador.
func Register(mux *http.ServeMux, motor Servicio) {
	mux.Handle("GET /dinamica-actual", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			estado, cuerpo, err := Actual(r.Context(), motor)
			if err != nil {
				errores.Responder(w, err) // Motor caido -> 503
				return
			}
			w.Header().Set("Cache-Control", "no-store") // la apuesta en vivo cambia
			httpx.EscribirCrudo(w, estado, "application/json", cuerpo)
		},
	)))
}

// Actual lee del Motor (GET /internal/dinamica-actual) el mercado dinamico
// vigente y devuelve el estado y el cuerpo tal cual, para reenviarlos. Solo
// 200 y 404 son validos; cualquier otro estado se trata como Motor caido (503).
func Actual(ctx context.Context, motor Servicio) (int, []byte, error) {
	resp, err := motor.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodGet,
		Ruta:   "/internal/dinamica-actual",
	})
	if err != nil {
		return 0, nil, err // ErrNoDisponible -> 503
	}
	if resp.Estado != http.StatusOK && resp.Estado != http.StatusNotFound {
		return 0, nil, interno.ErrNoDisponible
	}
	return resp.Estado, resp.Cuerpo, nil
}
