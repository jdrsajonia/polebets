package posiciones

import (
	"net/http"

	"github.com/arquisoft/motor/internal/httpx"
)

// Register monta GET /posiciones (HU-05): la ultima foto de la carrera en
// curso, para que el front no espere al primer mensaje del WebSocket ni al
// reconectarse. conSesion exige el JWT (cualquier rol); en main es
// a.ExigirSesion.
//
//	200 → FotoPosiciones
//	404 → {"error": "no hay una carrera en curso"}
func Register(mux *http.ServeMux, tablero *Tablero, conSesion func(http.Handler) http.Handler) {
	mux.Handle("GET /posiciones", conSesion(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		foto, ok := tablero.Foto()
		if !ok {
			httpx.EscribirJSON(w, http.StatusNotFound, map[string]string{"error": "no hay una carrera en curso"})
			return
		}
		httpx.EscribirJSON(w, http.StatusOK, foto)
	})))
}
