package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/arquisoft/motor/internal/ws"
)

// Si Autenticador deja de cumplir ws.Identificador, esto no compila.
var _ ws.Identificador = (*Autenticador)(nil)

// peticion arma un GET con la cookie de sesion del rol dado ("" = sin cookie).
func peticion(t *testing.T, rol string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if rol != "" {
		c := claimsValidos()
		c[ClaimRol] = rol
		r.AddCookie(&http.Cookie{Name: NombreCookie, Value: firmar(t, jwt.SigningMethodHS256, secretoPrueba, c)})
	}
	return r
}

// handlerOK responde 200 y guarda la sesion que vio en el contexto.
func handlerOK(vista *Sesion) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*vista, _ = SesionDe(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestExigirSesion(t *testing.T) {
	a := NuevoAutenticador(validador(t))

	t.Run("sin cookie da 401", func(t *testing.T) {
		var vista Sesion
		rec := httptest.NewRecorder()
		a.ExigirSesion(handlerOK(&vista)).ServeHTTP(rec, peticion(t, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("token invalido da 401", func(t *testing.T) {
		var vista Sesion
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: NombreCookie, Value: "token-falso"})
		rec := httptest.NewRecorder()
		a.ExigirSesion(handlerOK(&vista)).ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("con sesion deja user_id y rol en el contexto", func(t *testing.T) {
		var vista Sesion
		rec := httptest.NewRecorder()
		a.ExigirSesion(handlerOK(&vista)).ServeHTTP(rec, peticion(t, RolJugador))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, se esperaba 200", rec.Code)
		}
		if vista.UserID != userPrueba || vista.Rol != RolJugador {
			t.Errorf("sesion en el contexto = %+v", vista)
		}
	})
}

func TestExigirRol(t *testing.T) {
	a := NuevoAutenticador(validador(t))
	casos := []struct {
		nombre, rolUsuario string
		status             int
	}{
		{"admin entra", RolAdmin, http.StatusOK},
		{"jugador da 403", RolJugador, http.StatusForbidden},
		{"sin sesion da 401", "", http.StatusUnauthorized},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			var vista Sesion
			rec := httptest.NewRecorder()
			a.ExigirRol(RolAdmin, handlerOK(&vista)).ServeHTTP(rec, peticion(t, c.rolUsuario))
			if rec.Code != c.status {
				t.Errorf("status = %d, se esperaba %d", rec.Code, c.status)
			}
		})
	}
}

func TestIdentificar(t *testing.T) {
	a := NuevoAutenticador(validador(t))

	userID, err := a.Identificar(peticion(t, RolJugador))
	if err != nil || userID != userPrueba {
		t.Errorf("Identificar = %q, %v", userID, err)
	}

	if _, err := a.Identificar(peticion(t, "")); !errors.Is(err, ErrSinSesion) {
		t.Errorf("sin cookie: error = %v, se esperaba ErrSinSesion", err)
	}
}

func TestIdentificarConHeaderDelGateway(t *testing.T) {
	a := NuevoAutenticador(validador(t))
	const id = "0B6C2F1E-8D4A-4C3B-9F2E-1A2B3C4D5E6F"

	// Con X-User-Id del Gateway no hace falta la cookie.
	r := peticion(t, "")
	r.Header.Set(HeaderUsuarioGateway, id)
	if got, err := a.Identificar(r); err != nil || got != "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f" {
		t.Errorf("Identificar = %q, %v", got, err)
	}

	// El header manda sobre la cookie: es lo que valido el Gateway.
	r = peticion(t, RolJugador)
	r.Header.Set(HeaderUsuarioGateway, id)
	if got, _ := a.Identificar(r); got == userPrueba {
		t.Errorf("debio usar X-User-Id y uso la cookie")
	}

	for _, malo := range []string{"no-es-uuid", "0b6c2f1e8d4a4c3b9f2e1a2b3c4d5e6f", id + "x", "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6g"} {
		r = peticion(t, RolJugador)
		r.Header.Set(HeaderUsuarioGateway, malo)
		if _, err := a.Identificar(r); !errors.Is(err, ErrSinSesion) {
			t.Errorf("X-User-Id %q: error = %v, se esperaba ErrSinSesion", malo, err)
		}
	}
}

func TestSesionDeSinMiddleware(t *testing.T) {
	if _, ok := SesionDe(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Error("sin middleware no deberia haber sesion")
	}
}
