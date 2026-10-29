package sesion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arquisoft/orquestador/internal/errores"
)

const idJugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"

func TestExigirJugador(t *testing.T) {
	casos := []struct {
		nombre string
		id     string
		rol    string
		estado int
		codigo string // vacio si debe llegar al handler
	}{
		{"sin headers", "", "", 401, "sin_sesion"},
		{"sin rol", idJugador, "", 401, "sin_sesion"},
		{"sin id", "", RolJugador, 401, "sin_sesion"},
		{"id que no es uuid", "123", RolJugador, 401, "sin_sesion"},
		{"uuid con caracteres no hex", "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6z", RolJugador, 401, "sin_sesion"},
		{"uuid sin guiones", "0b6c2f1e8d4a4c3b9f2e1a2b3c4d5e6f0000", RolJugador, 401, "sin_sesion"},
		{"rol desconocido", idJugador, "jugador", 401, "sin_sesion"},
		{"rol en mayusculas", idJugador, "PLAYER", 401, "sin_sesion"},
		{"admin", idJugador, RolAdmin, 403, "solo_jugadores"},
		{"jugador", idJugador, RolJugador, 200, ""},
		{"jugador con uuid en mayusculas", "0B6C2F1E-8D4A-4C3B-9F2E-1A2B3C4D5E6F", RolJugador, 200, ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			var visto string
			llego := false
			h := ExigirJugador(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				llego = true
				visto, _ = UsuarioDe(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/eventos", nil)
			if c.id != "" {
				req.Header.Set(HeaderUsuario, c.id)
			}
			if c.rol != "" {
				req.Header.Set(HeaderRol, c.rol)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != c.estado {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, c.estado)
			}
			if c.codigo == "" {
				if !llego {
					t.Fatal("la peticion no llego al handler")
				}
				if visto != idJugador {
					t.Errorf("UsuarioDe = %q, se esperaba %q (en minusculas)", visto, idJugador)
				}
				return
			}
			if llego {
				t.Fatal("la peticion no debia llegar al handler")
			}
			var cuerpo errores.Cuerpo
			if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
				t.Fatalf("cuerpo no es JSON: %v", err)
			}
			if cuerpo.Codigo != c.codigo || cuerpo.Error == "" {
				t.Errorf("cuerpo = %+v, se esperaba codigo %s", cuerpo, c.codigo)
			}
		})
	}
}

func TestUsuarioDeSinMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if id, ok := UsuarioDe(req.Context()); ok || id != "" {
		t.Errorf("UsuarioDe = (%q, %v), se esperaba vacio", id, ok)
	}
}
