package dinamicas

import (
	"crypto/hmac"
	"net/http"

	"github.com/arquisoft/motor/internal/httpx"
)

// Register monta GET /internal/dinamica-actual (token de servicio): el mercado
// dinamico vigente (abierto, o el ultimo cerrado/resuelto de HU2-13), para que
// el Orquestador sirva la foto inicial al front sin esperar al reenvio del
// WebSocket (~5 s). token "" = no se monta la ruta.
//
//	200 → MensajeDinamico (el mismo payload que "mercado_dinamico")
//	404 → {"error": ...} si no hay un mercado dinamico vigente
func Register(mux *http.ServeMux, w *Worker, token string) {
	if token == "" {
		return
	}
	mux.HandleFunc("GET /internal/dinamica-actual", func(rw http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.Header.Get("X-Internal-Service-Token")), []byte(token)) {
			httpx.EscribirJSON(rw, http.StatusUnauthorized, map[string]string{"error": "servicio no autorizado"})
			return
		}
		m := w.MercadoActual()
		if m == nil {
			httpx.EscribirJSON(rw, http.StatusNotFound, map[string]string{"error": "no hay un mercado dinamico abierto"})
			return
		}
		httpx.EscribirJSON(rw, http.StatusOK, m)
	})
}
