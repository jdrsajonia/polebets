package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arquisoft/motor/internal/ws"
)

// Si Autenticador deja de cumplir ws.Identificador, esto no compila.
var _ ws.Identificador = (*Autenticador)(nil)

// peticion arma un GET con la identidad firmada del rol dado ("" = sin identidad).
func peticion(t *testing.T, rol string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if rol != "" {
		c := claimsValidos()
		c[ClaimRol] = rol
		r.Header.Set(HeaderIdentidad, firmar(t, privadaPrueba, c))
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

	t.Run("sin identidad da 401", func(t *testing.T) {
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
		r.Header.Set(HeaderIdentidad, "token-falso")
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
		t.Errorf("sin identidad: error = %v, se esperaba ErrSinSesion", err)
	}
}

func TestIdentificarIgnoraXUserId(t *testing.T) {
	a := NuevoAutenticador(validador(t))
	const otro = "0B6C2F1E-8D4A-4C3B-9F2E-1A2B3C4D5E6F"

	// El ataque del escenario 4: X-User-Id escrito a mano ya no identifica.
	r := peticion(t, "")
	r.Header.Set("X-User-Id", otro)
	if _, err := a.Identificar(r); !errors.Is(err, ErrSinSesion) {
		t.Errorf("solo X-User-Id: error = %v, se esperaba ErrSinSesion", err)
	}

	// Con identidad firmada, X-User-Id no cambia a quien se identifica.
	r = peticion(t, RolJugador)
	r.Header.Set("X-User-Id", otro)
	if got, err := a.Identificar(r); err != nil || got != userPrueba {
		t.Errorf("Identificar = %q, %v; se esperaba el sub firmado", got, err)
	}
}

func TestSesionDeSinMiddleware(t *testing.T) {
	if _, ok := SesionDe(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Error("sin middleware no deberia haber sesion")
	}
}
