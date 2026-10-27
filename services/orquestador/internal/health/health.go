// Package health expone el healthcheck del orquestador.
package health

import (
	"net/http"

	"github.com/arquisoft/orquestador/internal/httpx"
)

// Register agrega GET /health. No exige sesion ni consulta a Usuarios, Pagos
// o el Motor: solo dice que el proceso esta vivo. Si dependiera de ellos, una
// caida de Pagos marcaria tambien al orquestador como caido, aunque el
// registro o la pagina SSR puedan responder parcialmente.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.EscribirJSON(w, http.StatusOK, map[string]string{"estado": "ok"})
	})
}
