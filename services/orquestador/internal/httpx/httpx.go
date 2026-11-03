// Package httpx reune utilidades HTTP compartidas por los modulos del
// orquestador.
package httpx

import (
	"encoding/json"
	"net/http"
)

// EscribirJSON responde con el cuerpo serializado como JSON.
func EscribirJSON(w http.ResponseWriter, status int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

// EscribirCrudo reenvia un cuerpo ya serializado con su Content-Type. Se usa
// cuando el Orquestador solo hace de paso de una respuesta de otro servicio
// (p. ej. la foto de posiciones del Motor) y no necesita interpretarla.
func EscribirCrudo(w http.ResponseWriter, status int, contentType string, cuerpo []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(cuerpo)
}
