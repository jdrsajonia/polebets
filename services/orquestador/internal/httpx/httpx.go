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
