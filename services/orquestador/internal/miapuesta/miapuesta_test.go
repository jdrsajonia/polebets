package miapuesta

import (
	"encoding/json"
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
	mercado = "6f1c0a2b-3d4e-4f50-8a9b-0c1d2e3f4a5b"
	token   = "token-interno-de-pruebas-orquestador"
)

// pagosFalso responde con los estados de la lista, uno por llamada (el ultimo
// se repite), y guarda la ruta y el token que recibio.
type pagosFalso struct {
	llamadas atomic.Int32
	ruta     string
	tokenRx  string
}

func nuevoPagos(t *testing.T, cuerpo string, estados ...int) (*interno.Cliente, *pagosFalso) {
	t.Helper()
	f := &pagosFalso{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(f.llamadas.Add(1))
		f.ruta = r.Method + " " + r.URL.RequestURI()
		f.tokenRx = r.Header.Get("X-Internal-Service-Token")
		w.WriteHeader(estados[min(n, len(estados))-1])
		_, _ = w.Write([]byte(cuerpo))
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c, f
}

func pedir(t *testing.T, pagos Pagos, mercadoID, rol string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, pagos)
	req := httptest.NewRequest(http.MethodGet, "/mercados/"+mercadoID+"/mi-apuesta", nil)
	req.Header.Set(sesion.HeaderUsuario, jugador)
	if rol != "" {
		req.Header.Set(sesion.HeaderRol, rol)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func codigo(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var c struct {
		Codigo string `json:"codigo"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	return c.Codigo
}

func TestPendiente(t *testing.T) {
	pagos, f := nuevoPagos(t, `[{"bet_id":"b-1","user_id":"`+jugador+`","mode":"individual","option":"Sí",
		"amount":200,"multiplier":1.8,"status":"pending","payout":null}]`, http.StatusOK)

	rec := pedir(t, pagos, mercado, sesion.RolJugador)

	if rec.Code != http.StatusOK {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	if want := "GET /internal/markets/" + mercado + "/bets?user_id=" + jugador; f.ruta != want {
		t.Errorf("ruta a Pagos = %q, quiero %q", f.ruta, want)
	}
	if f.tokenRx != token {
		t.Errorf("token de servicio = %q", f.tokenRx)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	want := map[string]any{
		"apuesta_id": "b-1", "mercado_id": mercado, "opcion": "Sí", "monto": 200.0, "modo": "individual",
		"multiplicador": 1.8, "status": "pending", "payout": nil,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, quiero %v", k, got[k], v)
		}
	}
}

func TestLiquidadaDeTorneo(t *testing.T) {
	pagos, _ := nuevoPagos(t, `[{"bet_id":"b-1","mode":"torneo","option":"No","amount":50,
		"multiplier":1.25,"status":"won","payout":62}]`, http.StatusOK)

	rec := pedir(t, pagos, mercado, sesion.RolJugador)

	var a MiApuesta
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	if a.Modo != "torneo" || a.Status != "won" || a.Payout == nil || *a.Payout != 62 {
		t.Errorf("apuesta = %+v", a)
	}
}

func TestErrores(t *testing.T) {
	casos := []struct {
		nombre  string
		cuerpo  string
		estados []int
		rol     string
		estado  int
		codigo  string
		llamado bool
	}{
		{"no aposto", `[]`, []int{200}, sesion.RolJugador, 404, "sin_apuesta", true},
		{"mercado no existe", `{"detail":"no existe"}`, []int{404}, sesion.RolJugador, 404, "mercado_no_existe", true},
		{"mercado_id mal formado", `{"detail":[]}`, []int{422}, sesion.RolJugador, 404, "mercado_no_existe", true},
		{"pagos caido", ``, []int{503}, sesion.RolJugador, 503, "servicio_no_disponible", true},
		{"pagos 401", `{"detail":"token"}`, []int{401}, sesion.RolJugador, 500, "error_interno", true},
		{"sin sesion", `[]`, []int{200}, "", 401, "sin_sesion", false},
		{"admin", `[]`, []int{200}, sesion.RolAdmin, 403, "solo_jugadores", false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			pagos, f := nuevoPagos(t, c.cuerpo, c.estados...)
			rec := pedir(t, pagos, mercado, c.rol)
			if rec.Code != c.estado || codigo(t, rec) != c.codigo {
				t.Errorf("got %d %q, quiero %d %q (cuerpo %s)", rec.Code, codigo(t, rec), c.estado, c.codigo, rec.Body)
			}
			if (f.llamadas.Load() > 0) != c.llamado {
				t.Errorf("llamadas a Pagos = %d", f.llamadas.Load())
			}
		})
	}
}
