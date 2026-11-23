package auth

import (
	"context"
	"net/http"

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

// ExigirSesion responde 401 si la peticion no trae una identidad firmada
// valida (X-User-Token). Si la trae, deja el user_id y el rol en el contexto
// para el handler.
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

// Identificar cumple ws.Identificador: el WebSocket solo necesita saber a
// que usuario pertenece la conexion para enviarle sus resultados (HU-08).
//
// Escenario 4 (T3-05): el WebSocket entra por el Gateway (/api/ws), que valida
// la cookie con Usuarios y reenvia la identidad firmada (X-User-Token). Antes
// se aceptaba X-User-Id sin firma, y cualquier contenedor que llegara al motor
// podia abrir el WebSocket a nombre de otro jugador; tampoco se valida ya la
// cookie aqui, porque el motor no tiene JWT_SECRET.
func (a *Autenticador) Identificar(r *http.Request) (string, error) {
	s, err := DesdeRequest(a.validador, r)
	if err != nil {
		return "", err
	}
	return s.UserID, nil
}
