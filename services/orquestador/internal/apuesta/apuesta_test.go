package apuesta

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

const (
	jugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"
	mercado = "11111111-2222-4333-8444-555555555555"
	token   = "token-interno-de-pruebas-orquestador"
)

type motorFalso struct {
	llamadas atomic.Int32
	clave    string
	cuerpo   map[string]any
}

func nuevoMotor(t *testing.T, cuerpo string, estados ...int) (*interno.Cliente, *motorFalso) {
	t.Helper()
	f := &motorFalso{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(f.llamadas.Add(1))
		if r.URL.Path != "/internal/apuestas" || r.Header.Get("X-Internal-Service-Token") != token {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.clave = r.Header.Get("Idempotency-Key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &f.cuerpo)
		w.WriteHeader(estados[min(n, len(estados))-1])
		_, _ = w.Write([]byte(cuerpo))
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c, f
}

func pedir(t *testing.T, motor Motor, cuerpo, clave, rol string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, motor)
	req := httptest.NewRequest(http.MethodPost, "/apuestas", bytes.NewBufferString(cuerpo))
	req.Header.Set(sesion.HeaderUsuario, jugador)
	req.Header.Set(sesion.HeaderRol, rol)
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const okMotor = `{"apuesta_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","mercado_id":"` + mercado + `","opcion":"VER","monto":200,` +
	`"modo":"individual","multiplicador":2.5,"pago_si_acierta":500,"saldo":300,"fichas":null,"nueva":true}`

var apuesta = `{"mercado_id":"` + mercado + `","opcion":"VER","monto":200}`

func TestReenviaAlMotorConElUsuarioYLaClaveDelFront(t *testing.T) {
	motor, f := nuevoMotor(t, okMotor, http.StatusCreated)

	rec, out := pedir(t, motor, apuesta, "clave-front-1", sesion.RolJugador)

	if rec.Code != http.StatusCreated {
		t.Fatalf("estado = %d %s", rec.Code, rec.Body)
	}
	if f.clave != "clave-front-1" || f.cuerpo["user_id"] != jugador || f.cuerpo["mercado_id"] != mercado || f.cuerpo["monto"] != 200.0 {
		t.Errorf("clave %q, cuerpo %v", f.clave, f.cuerpo)
	}
	if out["modo"] != "individual" || out["multiplicador"] != 2.5 || out["pago_si_acierta"] != 500.0 || out["saldo"] != 300.0 || out["fichas"] != nil {
		t.Errorf("respuesta = %v", out)
	}
	if _, filtrado := out["nueva"]; filtrado {
		t.Errorf("no debe exponer campos internos: %v", out)
	}
}

func TestTraduceLosErroresDelMotor(t *testing.T) {
	for _, c := range []struct {
		codigo string
		estado int
	}{
		{"mercado_cerrado", 409}, {"ya_apostaste", 409}, {"clave_reutilizada", 409},
		{"monto_invalido", 422}, {"opcion_invalida", 422}, {"saldo_insuficiente", 422}, {"mercado_no_existe", 404},
	} {
		motor, _ := nuevoMotor(t, `{"error":"x","codigo":"`+c.codigo+`"}`, c.estado)
		rec, out := pedir(t, motor, apuesta, "k", sesion.RolJugador)
		if rec.Code != c.estado || out["codigo"] != c.codigo {
			t.Errorf("%s: %d %v", c.codigo, rec.Code, out)
		}
	}
}

func TestMotorCaidoReintentaYDa503(t *testing.T) {
	motor, f := nuevoMotor(t, `{"codigo":"servicio_no_disponible"}`, http.StatusServiceUnavailable)
	rec, out := pedir(t, motor, apuesta, "k", sesion.RolJugador)
	if rec.Code != http.StatusServiceUnavailable || out["codigo"] != "servicio_no_disponible" || f.llamadas.Load() != 3 {
		t.Fatalf("%d %v tras %d llamadas", rec.Code, out, f.llamadas.Load())
	}
}

func TestValidaAntesDeLlamarAlMotor(t *testing.T) {
	motor, f := nuevoMotor(t, okMotor, http.StatusCreated)
	casos := []struct {
		cuerpo, clave, rol string
		estado             int
		codigo             string
	}{
		{apuesta, "", sesion.RolJugador, 400, "falta_idempotency_key"},
		{`{"mercado_id":"x","opcion":"VER","monto":1,"extra":1}`, "k", sesion.RolJugador, 400, "cuerpo_invalido"},
		{`{"mercado_id":"","opcion":"VER","monto":1}`, "k", sesion.RolJugador, 400, "cuerpo_invalido"},
		{`{"mercado_id":"x","opcion":"VER","monto":0}`, "k", sesion.RolJugador, 422, "monto_invalido"},
		{apuesta, "k", sesion.RolAdmin, 403, "solo_jugadores"},
	}
	for _, c := range casos {
		rec, out := pedir(t, motor, c.cuerpo, c.clave, c.rol)
		if rec.Code != c.estado || out["codigo"] != c.codigo {
			t.Errorf("%s / %q: %d %v", c.cuerpo, c.clave, rec.Code, out)
		}
	}
	if f.llamadas.Load() != 0 {
		t.Errorf("llamo al Motor %d veces", f.llamadas.Load())
	}
}
