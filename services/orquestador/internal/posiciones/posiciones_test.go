package posiciones

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

const (
	jugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"
	token   = "token-interno-de-pruebas-orquestador"
)

// motorFalso responde GET /internal/posiciones con el estado y cuerpo fijados.
type motorFalso struct {
	estado int
	cuerpo string
	ruta   string
	tokenR string
}

func nuevoCliente(t *testing.T, f *motorFalso) *interno.Cliente {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.ruta = r.URL.Path
		f.tokenR = r.Header.Get("X-Internal-Service-Token")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.estado)
		_, _ = w.Write([]byte(f.cuerpo))
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c
}

func pedir(t *testing.T, motor Servicio, rol string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, motor)
	req := httptest.NewRequest(http.MethodGet, "/posiciones", nil)
	if jugador != "" {
		req.Header.Set(sesion.HeaderUsuario, jugador)
	}
	if rol != "" {
		req.Header.Set(sesion.HeaderRol, rol)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestConCarreraReenviaLaFoto(t *testing.T) {
	foto := `{"carrera_id":"9590","pilotos":[{"posicion":1,"codigo":"LEC"}]}`
	f := &motorFalso{estado: http.StatusOK, cuerpo: foto}
	rec := pedir(t, nuevoCliente(t, f), sesion.RolJugador)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200: %s", rec.Code, rec.Body)
	}
	if rec.Body.String() != foto {
		t.Fatalf("la foto no se reenvio tal cual: %s", rec.Body.String())
	}
	if f.ruta != "/internal/posiciones" || f.tokenR != token {
		t.Fatalf("llamada al Motor: ruta=%q token=%q", f.ruta, f.tokenR)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("la foto cambia en vivo: debe ser no-store")
	}
}

func TestSinCarreraReenviaEl404(t *testing.T) {
	f := &motorFalso{estado: http.StatusNotFound, cuerpo: `{"error":"no hay una carrera en curso"}`}
	rec := pedir(t, nuevoCliente(t, f), sesion.RolJugador)

	// El 404 del Motor se reenvia tal cual: el Front lo lee como "sin carrera".
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, se esperaba 404: %s", rec.Code, rec.Body)
	}
}

func TestMotorCaidoEs503(t *testing.T) {
	// 500 del Motor (ni 200 ni 404) -> servicio no disponible.
	f := &motorFalso{estado: http.StatusInternalServerError, cuerpo: ""}
	rec := pedir(t, nuevoCliente(t, f), sesion.RolJugador)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, se esperaba 503: %s", rec.Code, rec.Body)
	}
}

func TestSoloJugadores(t *testing.T) {
	f := &motorFalso{estado: http.StatusOK, cuerpo: `{}`}
	rec := pedir(t, nuevoCliente(t, f), "admin")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, se esperaba 403 para no-jugador", rec.Code)
	}
}
