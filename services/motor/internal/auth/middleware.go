package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/arquisoft/motor/internal/httpx"
)

// Autenticador protege las rutas REST y el WebSocket del motor (RNF-06).
//
// Uso en un modulo:
//
//	mux.Handle("POST /apuestas", a.ExigirSesion(http.HandlerFunc(apostar)))
//	mux.Handle("POST /mercados", a.ExigirRol(auth.RolAdmin, http.HandlerFunc(abrir)))
//
// y dentro del handler: sesion, _ := auth.SesionDe(r.Context())
type Autenticador struct {
	validador ValidarToken
}

func NuevoAutenticador(v ValidarToken) *Autenticador {
	return &Autenticador{validador: v}
}

type claveSesion struct{}

// SesionDe devuelve la sesion que dejo ExigirSesion en el contexto.
func SesionDe(ctx context.Context) (Sesion, bool) {
	s, ok := ctx.Value(claveSesion{}).(Sesion)
	return s, ok
}

// ExigirSesion responde 401 si la peticion no trae un JWT valido. Si lo trae,
// deja el user_id y el rol en el contexto para el handler.
func (a *Autenticador) ExigirSesion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := DesdeRequest(a.validador, r)
		if err != nil {
			httpx.EscribirJSON(w, http.StatusUnauthorized, map[string]string{"error": "no autorizado"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveSesion{}, s)))
	})
}

// ExigirRol exige sesion (401) y ademas el rol indicado (403). Por ejemplo,
// solo el administrador abre mercados y solo los jugadores apuestan (HU-12).
func (a *Autenticador) ExigirRol(rol string, next http.Handler) http.Handler {
	return a.ExigirSesion(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, _ := SesionDe(r.Context()); s.Rol != rol {
			httpx.EscribirJSON(w, http.StatusForbidden, map[string]string{"error": "no tienes permiso para esta accion"})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// HeaderUsuarioGateway es el header que pone el API Gateway despues de
// validar la sesion con auth_request contra Usuarios (T2-01, HU2-02).
const HeaderUsuarioGateway = "X-User-Id"

// Identificar cumple ws.Identificador: el WebSocket solo necesita saber a
// que usuario pertenece la conexion para enviarle sus resultados (HU-08).
//
// Prototipo 2 (HU2-02): el WebSocket entra por el Gateway (/api/ws), que ya
// valido la cookie y pone X-User-Id; el Gateway borra el que mande el cliente
// y el Motor no esta publicado al host, asi que ese header es confiable. Si
// no viene (pruebas locales sin Gateway), se valida el JWT de la cookie como
// en el prototipo 1.
func (a *Autenticador) Identificar(r *http.Request) (string, error) {
	if id := strings.TrimSpace(r.Header.Get(HeaderUsuarioGateway)); id != "" {
		if !esUUID(id) {
			return "", ErrSinSesion
		}
		return strings.ToLower(id), nil
	}
	s, err := DesdeRequest(a.validador, r)
	if err != nil {
		return "", err
	}
	return s.UserID, nil
}

// esUUID acepta el formato 8-4-4-4-12 en hexadecimal.
func esUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return true
}
