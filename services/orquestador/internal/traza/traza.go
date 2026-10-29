// Package traza le da a cada peticion un X-Request-Id para seguir un flujo
// en los logs de todos los servicios (seccion 4 de
// documentation/orquestador-api.md).
//
// Si el Gateway ya trae uno, se reusa; si no, se genera. El id vuelve en la
// respuesta y el cliente interno lo reenvia a Usuarios, Pagos y Motor.
package traza

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// Header es el nombre del header del id de la peticion.
const Header = "X-Request-Id"

// LargoMaximo evita que un id gigante llegue a los logs.
const LargoMaximo = 128

type claveID struct{}

// De devuelve el id de la peticion, o "" si el contexto no tiene uno.
func De(ctx context.Context) string {
	id, _ := ctx.Value(claveID{}).(string)
	return id
}

// EnContexto devuelve un contexto con el id de la peticion.
func EnContexto(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, claveID{}, id)
}

// Middleware toma el X-Request-Id que llega (si es valido) o genera uno, lo
// pone en la respuesta y lo deja en el contexto.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(Header)
		if !valido(id) {
			id = Nuevo()
		}
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r.WithContext(EnContexto(r.Context(), id)))
	})
}

// Nuevo genera un id aleatorio de 32 caracteres hexadecimales.
func Nuevo() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // sin entropia del sistema no se puede continuar
	}
	return hex.EncodeToString(b[:])
}

// valido acepta ids de 1 a 128 caracteres ASCII visibles (sin espacios ni
// saltos de linea), para que nadie pueda meter lineas falsas en los logs.
func valido(id string) bool {
	if id == "" || len(id) > LargoMaximo {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}
