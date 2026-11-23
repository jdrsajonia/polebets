// Package sesiontest firma identidades de prueba para los tests de los
// modulos que usan sesion.ExigirJugador (como httptest para net/http).
//
// La primera llamada genera un par Ed25519 propio de la corrida y configura
// sesion con su llave publica; nunca se usa la llave del compose.
//
// Uso en un test:
//
//	sesiontest.Identificar(req, idJugador, sesion.RolJugador)
package sesiontest

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/arquisoft/orquestador/internal/sesion"
)

var (
	una     sync.Once
	privada ed25519.PrivateKey
)

// llave genera el par de prueba y configura sesion la primera vez.
func llave() ed25519.PrivateKey {
	una.Do(func() {
		publica, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic(err)
		}
		privada = priv
		sesion.Configurar(publica)
	})
	return privada
}

// Token firma una identidad como lo haria Usuarios (vigente por un minuto).
// Un id o rol vacio quedan vacios en el token, para probar los rechazos.
func Token(userID, rol string) string {
	ahora := time.Now()
	claims := jwt.MapClaims{
		"sub":           userID,
		sesion.ClaimRol: rol,
		"iss":           sesion.Emisor,
		"aud":           sesion.Audiencia,
		"iat":           ahora.Unix(),
		"exp":           ahora.Add(time.Minute).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(llave())
	if err != nil {
		panic(err)
	}
	return token
}

// Identificar pone en la peticion la identidad firmada que pondria el
// Gateway. Si el id y el rol estan vacios no pone nada (peticion sin sesion).
func Identificar(r *http.Request, userID, rol string) {
	llave() // configura sesion aunque la peticion vaya sin identidad
	if userID == "" && rol == "" {
		return
	}
	r.Header.Set(sesion.HeaderIdentidad, Token(userID, rol))
}
