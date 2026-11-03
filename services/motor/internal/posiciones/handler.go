package posiciones

import (
	"crypto/hmac"
	"net/http"

	"github.com/arquisoft/motor/internal/httpx"
)

// Register monta las rutas de la foto de posiciones (HU-05 / HU2-14): la
// ultima foto de la carrera en curso, para que el front no espere al primer
// mensaje del WebSocket ni al reconectarse.
//
//	GET /posiciones          exige el JWT del jugador (uso historico/directo)
//	GET /internal/posiciones token de servicio: la llama el Orquestador para
//	                         servir la foto inicial por el Gateway (sin exponer
//	                         REST del Motor al cliente). token "" = no se monta.
//
// Ambas devuelven: 200 FotoPosiciones · 404 {"error": ...} si no hay carrera.
func Register(mux *http.ServeMux, tablero *Tablero, conSesion func(http.Handler) http.Handler, token string) {
	mux.Handle("GET /posiciones", conSesion(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		escribirFoto(w, tablero)
	})))

	if token != "" {
		mux.HandleFunc("GET /internal/posiciones", func(w http.ResponseWriter, r *http.Request) {
			if !hmac.Equal([]byte(r.Header.Get("X-Internal-Service-Token")), []byte(token)) {
				httpx.EscribirJSON(w, http.StatusUnauthorized, map[string]string{"error": "servicio no autorizado"})
				return
			}
			escribirFoto(w, tablero)
		})
	}
}

// escribirFoto responde la ultima foto del tablero, o 404 si no hay carrera.
func escribirFoto(w http.ResponseWriter, tablero *Tablero) {
	foto, ok := tablero.Foto()
	if !ok {
		httpx.EscribirJSON(w, http.StatusNotFound, map[string]string{"error": "no hay una carrera en curso"})
		return
	}
	httpx.EscribirJSON(w, http.StatusOK, foto)
}
