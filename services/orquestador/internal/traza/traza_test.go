package traza

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada string
		reusa   bool
	}{
		{"reusa el del Gateway", "gw-1234-abcd", true},
		{"genera si no llega", "", false},
		{"genera si es muy largo", strings.Repeat("a", LargoMaximo+1), false},
		{"genera si trae espacios", "id con espacios", false},
		{"genera si trae salto de linea", "id\nfalso", false},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			var enContexto string
			h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				enContexto = De(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.entrada != "" {
				req.Header.Set(Header, c.entrada)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			enRespuesta := rec.Header().Get(Header)
			if enContexto == "" || enContexto != enRespuesta {
				t.Fatalf("contexto = %q, respuesta = %q; deben ser iguales y no vacios", enContexto, enRespuesta)
			}
			if c.reusa && enContexto != c.entrada {
				t.Errorf("id = %q, se esperaba reusar %q", enContexto, c.entrada)
			}
			if !c.reusa && (enContexto == c.entrada || len(enContexto) != 32) {
				t.Errorf("id = %q, se esperaba uno nuevo de 32 caracteres", enContexto)
			}
		})
	}
}

func TestDeSinMiddleware(t *testing.T) {
	if id := De(httptest.NewRequest(http.MethodGet, "/", nil).Context()); id != "" {
		t.Errorf("De = %q, se esperaba vacio", id)
	}
}
