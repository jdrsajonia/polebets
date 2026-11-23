// Package sesion identifica al jugador con la identidad firmada que reenvia el
// API Gateway (seccion 2 de documentation/orquestador-api.md).
//
// El Gateway valida la cookie con auth_request contra Usuarios, y Usuarios le
// devuelve un token corto (X-User-Token) con el id y el rol, firmado con su
// llave privada Ed25519. Aqui se verifica esa firma con la llave PUBLICA antes
// de atender la peticion.
//
// Escenario 4 (T3-05): antes se confiaba en la cabecera X-User-Id, y cualquier
// contenedor que llegara al orquestador podia escribirla con el id de otro
// jugador. Una firma no se puede escribir a mano: sin la llave privada, que
// solo tiene Usuarios, el token no verifica.
//
// Uso en un modulo:
//
//	mux.Handle("POST /apuestas", sesion.ExigirJugador(http.HandlerFunc(apostar)))
//
// y dentro del handler: userID, _ := sesion.UsuarioDe(r.Context())
//
// main llama Configurar con la llave publica antes de servir. Las rutas
// publicas (GET /health, POST /registro) se registran sin el middleware.
package sesion

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/golang-jwt/jwt/v5"

	"github.com/arquisoft/orquestador/internal/errores"
)

// HeaderIdentidad es donde el Gateway reenvia la identidad firmada.
const HeaderIdentidad = "X-User-Token"

// Roles validos: los valores de users.role.
const (
	RolJugador = "player"
	RolAdmin   = "admin"
)

// Contrato del token con Usuarios (services/usuarios/app/core/identidad.py).
const (
	Algoritmo = "EdDSA"
	Emisor    = "usuarios"
	Audiencia = "polebet"
	ClaimRol  = "rol"
)

// llavePublica es la llave con la que se verifican los tokens. Hasta que main
// llame Configurar esta vacia y toda peticion recibe 401 (falla cerrado).
var llavePublica atomic.Pointer[ed25519.PublicKey]

// parser fija el algoritmo (rechaza "none", HS256 y cualquier otro), exige
// exp y comprueba el emisor y la audiencia.
var parser = jwt.NewParser(
	jwt.WithValidMethods([]string{Algoritmo}),
	jwt.WithExpirationRequired(),
	jwt.WithIssuer(Emisor),
	jwt.WithAudience(Audiencia),
)

// Configurar fija la llave publica de Usuarios. Se llama una vez al arrancar.
func Configurar(llave ed25519.PublicKey) {
	llavePublica.Store(&llave)
}

// LeerLlavePublica carga la llave publica Ed25519 (PEM) del archivo dado.
func LeerLlavePublica(ruta string) (ed25519.PublicKey, error) {
	pem, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la llave publica de identidad: %w", err)
	}
	llave, err := jwt.ParseEdPublicKeyFromPEM(pem)
	if err != nil {
		return nil, fmt.Errorf("la llave de %s no es una llave publica Ed25519: %w", ruta, err)
	}
	publica, ok := llave.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("la llave de %s no es una llave publica Ed25519", ruta)
	}
	return publica, nil
}

type claveUsuario struct{}

// UsuarioDe devuelve el user_id que dejo ExigirJugador en el contexto.
func UsuarioDe(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(claveUsuario{}).(string)
	return id, ok
}

// ExigirJugador deja pasar solo peticiones de un jugador:
//
//	sin X-User-Token, firma invalida o vencido  -> 401 sin_sesion
//	sub que no es uuid o rol desconocido        -> 401 sin_sesion
//	rol = admin                                 -> 403 solo_jugadores (el admin usa la CLI)
//	rol = player                                -> sigue, con el user_id en el contexto
func ExigirJugador(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, rol, err := verificar(r.Header.Get(HeaderIdentidad))
		if err != nil {
			errores.Responder(w, errores.SinSesion)
			return
		}
		switch rol {
		case RolJugador:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveUsuario{}, id)))
		case RolAdmin:
			errores.Responder(w, errores.SoloJugadores)
		default:
			errores.Responder(w, errores.SinSesion)
		}
	})
}

var errIdentidad = errors.New("identidad invalida")

// verificar revisa firma, algoritmo, vencimiento, emisor y audiencia, y
// devuelve el user_id (en minusculas) y el rol del token.
func verificar(token string) (string, string, error) {
	llave := llavePublica.Load()
	if llave == nil || strings.TrimSpace(token) == "" {
		return "", "", errIdentidad
	}
	claims := jwt.MapClaims{}
	if _, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return *llave, nil
	}); err != nil {
		return "", "", fmt.Errorf("%w: %w", errIdentidad, err)
	}
	sub, _ := claims["sub"].(string)
	id := strings.ToLower(strings.TrimSpace(sub))
	if !esUUID(id) {
		return "", "", errIdentidad
	}
	rol, _ := claims[ClaimRol].(string)
	return id, rol, nil
}

// esUUID revisa el formato 8-4-4-4-12 en hexadecimal. No exige version: el id
// lo genera Usuarios (o el orquestador en el registro) y aqui solo importa
// que sea un uuid bien formado.
func esUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
				return false
			}
		}
	}
	return true
}
