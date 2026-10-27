// Package sesion identifica al jugador con los headers que pone el API
// Gateway (seccion 2 de documentation/orquestador-api.md).
//
// El orquestador no valida el JWT: eso lo hace el Gateway con auth_request
// contra Usuarios y luego agrega X-User-Id y X-User-Rol. El Gateway borra los
// que mande el cliente, asi que aqui se confia en ellos.
//
// Uso en un modulo:
//
//	mux.Handle("POST /apuestas", sesion.ExigirJugador(http.HandlerFunc(apostar)))
//
// y dentro del handler: userID, _ := sesion.UsuarioDe(r.Context())
//
// Las rutas publicas (GET /health, POST /registro) se registran sin el
// middleware.
package sesion

import (
	"context"
	"net/http"
	"strings"

	"github.com/arquisoft/orquestador/internal/errores"
)

// Headers que pone el Gateway.
const (
	HeaderUsuario = "X-User-Id"
	HeaderRol     = "X-User-Rol"
)

// Roles validos: los valores de users.role.
const (
	RolJugador = "player"
	RolAdmin   = "admin"
)

type claveUsuario struct{}

// UsuarioDe devuelve el user_id que dejo ExigirJugador en el contexto.
func UsuarioDe(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(claveUsuario{}).(string)
	return id, ok
}

// ExigirJugador deja pasar solo peticiones de un jugador:
//
//	X-User-Id vacio o que no es uuid      -> 401 sin_sesion
//	X-User-Rol vacio o que no es un rol   -> 401 sin_sesion
//	X-User-Rol = admin                    -> 403 solo_jugadores (el admin usa la CLI)
//	X-User-Rol = player                   -> sigue, con el user_id en el contexto
func ExigirJugador(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderUsuario)))
		if !esUUID(id) {
			errores.Responder(w, errores.SinSesion)
			return
		}
		switch strings.TrimSpace(r.Header.Get(HeaderRol)) {
		case RolJugador:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveUsuario{}, id)))
		case RolAdmin:
			errores.Responder(w, errores.SoloJugadores)
		default:
			errores.Responder(w, errores.SinSesion)
		}
	})
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
