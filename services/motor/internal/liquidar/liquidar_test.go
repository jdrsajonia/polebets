package liquidar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const token = "token-interno-de-pruebas-del-motor-32c"

// pagosFalso imita las rutas de Pagos que usa HU2-10, con estado: las
// apuestas pagadas dejan de estar pendientes, la misma clave no paga dos
// veces y el mercado solo pasa a liquidado sin pendientes.
type pagosFalso struct {
	mu        sync.Mutex
	mercados  map[string]*Mercado
	apuestas  map[string]*Apuesta // por bet_id
	saldos    map[string]int64    // por user_id
	claves    map[string]bool
	fallarBet string // bet_id cuyo settlement responde 503
	pagos     int
}

func nuevoPagos() *pagosFalso {
	ver := "VER"
	return &pagosFalso{
		mercados: map[string]*Mercado{
			"m1": {ID: "m1", Pregunta: "¿Quién gana?", Estado: "cerrado", Ganadora: &ver},
		},
		apuestas: map[string]*Apuesta{
			"b-gana":   {ID: "b-gana", UserID: "u1", Modo: "individual", Opcion: "VER", Monto: 100, Multiplicador: 2.5, Estado: "pending"},
			"b-pierde": {ID: "b-pierde", UserID: "u2", Modo: "individual", Opcion: "NOR", Monto: 50, Multiplicador: 3, Estado: "pending"},
		},
		saldos: map[string]int64{"u1": 900, "u2": 950},
		claves: map[string]bool{},
	}
}

func (f *pagosFalso) servidor(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-Internal-Service-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ruta := r.URL.Path
		switch {
		case r.Method == http.MethodGet && ruta == "/internal/markets":
			if r.URL.Query().Get("status") != "cerrado" || r.URL.Query().Get("resuelto") != "true" {
				t.Errorf("filtros = %s", r.URL.RawQuery)
			}
			var out []Mercado
			for _, m := range f.mercados {
				if m.Estado == "cerrado" && m.Ganadora != nil {
					out = append(out, *m)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodGet && strings.HasSuffix(ruta, "/bets"):
			if r.URL.Query().Get("mode") != "individual" || r.URL.Query().Get("status") != "pending" {
				t.Errorf("filtros de apuestas = %s", r.URL.RawQuery)
			}
			out := []Apuesta{}
			for _, a := range f.apuestas {
				if a.Estado == "pending" {
					out = append(out, *a)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && ruta == "/internal/settlements":
			var in struct{ BetID, Result, IdempotencyKey string }
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &struct {
				BetID          *string `json:"bet_id"`
				Result         *string `json:"result"`
				IdempotencyKey *string `json:"idempotency_key"`
			}{&in.BetID, &in.Result, &in.IdempotencyKey})
			if clave := r.Header.Get("Idempotency-Key"); clave != "bet-settlement:"+in.BetID || in.IdempotencyKey != clave {
				t.Errorf("clave = %q cuerpo %q", clave, in.IdempotencyKey)
			}
			if in.BetID == f.fallarBet {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			a := f.apuestas[in.BetID]
			estado := http.StatusOK
			if !f.claves[in.IdempotencyKey] {
				f.claves[in.IdempotencyKey] = true
				f.pagos++
				estado = http.StatusCreated
				a.Estado = in.Result
				if in.Result == "won" {
					f.saldos[a.UserID] += int64(float64(a.Monto) * a.Multiplicador)
				}
			}
			var pago int64
			if a.Estado == "won" {
				pago = int64(float64(a.Monto) * a.Multiplicador)
			}
			w.WriteHeader(estado)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bet_id": a.ID, "status": a.Estado, "payout": pago, "saldo": f.saldos[a.UserID], "nueva": estado == http.StatusCreated,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(ruta, "/liquidated"):
			id := strings.Split(ruta, "/")[3]
			for _, a := range f.apuestas {
				if a.Estado == "pending" {
					w.WriteHeader(http.StatusConflict)
					return
				}
			}
			f.mercados[id].Estado = "liquidado"
			_, _ = w.Write([]byte(`{"status":"liquidado"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type avisos struct {
	mu  sync.Mutex
	por map[string][]Resultado
}

func (a *avisos) EnviarA(userID, tipo string, payload any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if tipo != TipoResultadoApuesta {
		panic("tipo inesperado " + tipo)
	}
	if a.por == nil {
		a.por = map[string][]Resultado{}
	}
	a.por[userID] = append(a.por[userID], payload.(Resultado))
	return nil
}

func armar(t *testing.T, f *pagosFalso) (*Worker, *avisos) {
	srv := f.servidor(t)
	av := &avisos{}
	return NuevoWorker(NuevoPagosHTTP(srv.URL, token), av, 0), av
}

func TestPagaAlGanadorCobraAlPerdedorYMarcaLiquidado(t *testing.T) {
	f := nuevoPagos()
	w, av := armar(t, f)

	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}

	if f.apuestas["b-gana"].Estado != "won" || f.apuestas["b-pierde"].Estado != "lost" {
		t.Errorf("estados = %s / %s", f.apuestas["b-gana"].Estado, f.apuestas["b-pierde"].Estado)
	}
	if f.saldos["u1"] != 1150 || f.saldos["u2"] != 950 {
		t.Errorf("saldos = %v", f.saldos)
	}
	if f.mercados["m1"].Estado != "liquidado" {
		t.Errorf("mercado = %s", f.mercados["m1"].Estado)
	}
	g := av.por["u1"]
	if len(g) != 1 || !g[0].Gano || g[0].Pago != 250 || g[0].Saldo != 1150 || g[0].Ganadora != "VER" || g[0].Modo != "individual" || g[0].Pregunta != "¿Quién gana?" {
		t.Errorf("aviso ganador = %+v", g)
	}
	p := av.por["u2"]
	if len(p) != 1 || p[0].Gano || p[0].Pago != 0 || p[0].Opcion != "NOR" {
		t.Errorf("aviso perdedor = %+v", p)
	}
}

func TestOtraVueltaNoPagaNiAvisaDosVeces(t *testing.T) {
	f := nuevoPagos()
	w, av := armar(t, f)

	_ = w.Procesar(context.Background())
	_ = w.Procesar(context.Background())

	if f.pagos != 2 || len(av.por["u1"]) != 1 || len(av.por["u2"]) != 1 {
		t.Errorf("pagos = %d avisos = %v", f.pagos, av.por)
	}
}

func TestSiUnPagoFallaElMercadoSeReintentaSinPagarDoble(t *testing.T) {
	f := nuevoPagos()
	f.fallarBet = "b-pierde"
	w, av := armar(t, f)

	if err := w.Procesar(context.Background()); err == nil {
		t.Error("esperaba error por el pago fallido")
	}
	if f.mercados["m1"].Estado != "cerrado" || f.apuestas["b-gana"].Estado != "won" {
		t.Fatalf("tras el fallo: mercado %s, b-gana %s", f.mercados["m1"].Estado, f.apuestas["b-gana"].Estado)
	}

	f.fallarBet = "" // Pagos se recupera
	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.mercados["m1"].Estado != "liquidado" || f.pagos != 2 || f.saldos["u1"] != 1150 || len(av.por["u1"]) != 1 {
		t.Errorf("tras reintentar: mercado %s pagos %d saldos %v avisos %v", f.mercados["m1"].Estado, f.pagos, f.saldos, av.por)
	}
}

func TestMercadoSinGanadorNoSeToca(t *testing.T) {
	f := nuevoPagos()
	f.mercados["m1"].Ganadora = nil
	w, av := armar(t, f)

	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.pagos != 0 || f.mercados["m1"].Estado != "cerrado" || len(av.por) != 0 {
		t.Errorf("no debia liquidar: pagos %d estado %s", f.pagos, f.mercados["m1"].Estado)
	}
}

func TestSinTokenPagosRechazaYNoSePagaNada(t *testing.T) {
	f := nuevoPagos()
	srv := f.servidor(t)
	w := NuevoWorker(NuevoPagosHTTP(srv.URL, "otro-token"), nil, 0)

	if err := w.Procesar(context.Background()); err == nil {
		t.Error("esperaba error con token incorrecto")
	}
	if f.pagos != 0 {
		t.Errorf("pagos = %d", f.pagos)
	}
}
