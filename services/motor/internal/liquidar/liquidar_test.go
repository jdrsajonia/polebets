package liquidar

import (
	"context"
	"encoding/json"
	"errors"
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
	fallarBet string // bet_id cuyo settlement (individual o torneo) responde 503
	pagos     int
	marcas    int // apuestas de torneo marcadas en Pagos (nuevas)
}

func nuevoPagos() *pagosFalso {
	ver := "VER"
	return &pagosFalso{
		mercados: map[string]*Mercado{
			"m1": {ID: "m1", EventoID: "e1", Pregunta: "¿Quién gana?", Estado: "cerrado", Ganadora: &ver},
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
			modo := r.URL.Query().Get("mode")
			if (modo != "individual" && modo != "torneo") || r.URL.Query().Get("status") != "pending" {
				t.Errorf("filtros de apuestas = %s", r.URL.RawQuery)
			}
			out := []Apuesta{}
			for _, a := range f.apuestas {
				if a.Estado == "pending" && a.Modo == modo {
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
		case r.Method == http.MethodPost && strings.HasPrefix(ruta, "/internal/bets/torneo/") && strings.HasSuffix(ruta, "/settlement"):
			id := strings.Split(ruta, "/")[4]
			var in struct {
				Result string `json:"result"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &in)
			if id == f.fallarBet {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			a := f.apuestas[id]
			if a == nil || a.Modo != "torneo" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			estado := http.StatusOK
			switch a.Estado {
			case "pending":
				a.Estado = in.Result
				f.marcas++
				estado = http.StatusCreated
			case in.Result:
			default:
				w.WriteHeader(http.StatusConflict)
				return
			}
			var pago int64
			if a.Estado == "won" {
				pago = int64(float64(a.Monto) * a.Multiplicador)
			}
			w.WriteHeader(estado)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": a.Estado, "payout": pago, "nueva": estado == http.StatusCreated})
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
	mu     sync.Mutex
	por    map[string][]Resultado
	torneo map[string][]ResultadoTorneo
}

func (a *avisos) EnviarA(userID, tipo string, payload any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if tipo != TipoResultadoApuesta {
		panic("tipo inesperado " + tipo)
	}
	if a.por == nil {
		a.por, a.torneo = map[string][]Resultado{}, map[string][]ResultadoTorneo{}
	}
	switch r := payload.(type) {
	case Resultado:
		a.por[userID] = append(a.por[userID], r)
	case ResultadoTorneo:
		a.torneo[userID] = append(a.torneo[userID], r)
	}
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

// fichasFalsas imita el almacen de HU2-11: el resultado de cada apuesta se
// escribe una sola vez y el total se recalcula (no se suma).
type fichasFalsas struct {
	inicial    int64
	reservas   map[string]Apuesta // bet_id -> apuesta reservada
	resultados map[string]int64   // bet_id -> fichas pagadas
	falla      error              // != nil: Redis no responde
	difusiones int
}

func nuevasFichas(reservas ...Apuesta) *fichasFalsas {
	f := &fichasFalsas{inicial: 1000, reservas: map[string]Apuesta{}, resultados: map[string]int64{}}
	for _, a := range reservas {
		f.reservas[a.ID] = a
	}
	return f
}

func (f *fichasFalsas) total(userID string) int64 {
	t := f.inicial
	for id, a := range f.reservas {
		if a.UserID == userID {
			t += f.resultados[id] - a.Monto
		}
	}
	return t
}

func (f *fichasFalsas) LiquidarApuesta(_ context.Context, eventoID, apuestaID string, gano bool, _ float64) (FichasTorneo, error) {
	if f.falla != nil {
		return FichasTorneo{}, f.falla
	}
	a, ok := f.reservas[apuestaID]
	if !ok || eventoID != "e1" {
		return FichasTorneo{}, ErrSinReserva
	}
	if _, ya := f.resultados[apuestaID]; !ya {
		var pagadas int64
		if gano {
			pagadas = int64(float64(a.Monto) * a.Multiplicador)
		}
		f.resultados[apuestaID] = pagadas
	}
	return FichasTorneo{Ganadas: f.resultados[apuestaID], Total: f.total(a.UserID), Puesto: 1}, nil
}

func (f *fichasFalsas) Difundir(context.Context, string) { f.difusiones++ }

// mixto agrega al mercado m1 dos apuestas de torneo (u3 acierta, u4 falla) a
// las dos individuales de nuevoPagos.
func mixto() (*pagosFalso, *fichasFalsas) {
	f := nuevoPagos()
	gana := &Apuesta{ID: "t-gana", UserID: "u3", Modo: "torneo", Opcion: "VER", Monto: 200, Multiplicador: 2.5, Estado: "pending"}
	pierde := &Apuesta{ID: "t-pierde", UserID: "u4", Modo: "torneo", Opcion: "NOR", Monto: 300, Multiplicador: 3, Estado: "pending"}
	f.apuestas[gana.ID], f.apuestas[pierde.ID] = gana, pierde
	return f, nuevasFichas(*gana, *pierde)
}

func armarConFichas(t *testing.T, f *pagosFalso, fx Fichas) (*Worker, *avisos) {
	w, av := armar(t, f)
	w.Fichas = fx
	return w, av
}

func TestMercadoMixtoLiquidaLosDosModosYAvisaConFichasYPuesto(t *testing.T) {
	f, fx := mixto()
	w, av := armarConFichas(t, f, fx)

	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}

	if f.apuestas["b-gana"].Estado != "won" || f.apuestas["t-gana"].Estado != "won" || f.apuestas["t-pierde"].Estado != "lost" {
		t.Errorf("estados = %s %s %s", f.apuestas["b-gana"].Estado, f.apuestas["t-gana"].Estado, f.apuestas["t-pierde"].Estado)
	}
	if f.mercados["m1"].Estado != "liquidado" || f.saldos["u1"] != 1150 || f.pagos != 2 {
		t.Errorf("mercado %s, saldos %v, pagos en tokens %d", f.mercados["m1"].Estado, f.saldos, f.pagos)
	}
	g := av.torneo["u3"]
	if len(g) != 1 || !g[0].Gano || g[0].Modo != "torneo" || g[0].FichasGanadas != 500 || g[0].FichasPerdidas != 0 || g[0].FichasTotal != 1300 || g[0].Puesto != 1 {
		t.Errorf("aviso torneo ganador = %+v", g)
	}
	p := av.torneo["u4"]
	if len(p) != 1 || p[0].Gano || p[0].FichasGanadas != 0 || p[0].FichasPerdidas != 300 || p[0].FichasTotal != 700 {
		t.Errorf("aviso torneo perdedor = %+v", p)
	}
	if fx.difusiones != 1 {
		t.Errorf("ranking difundido %d veces, quiero 1", fx.difusiones)
	}
}

func TestLiquidacionDeTorneoRepetidaNoCambiaLasFichas(t *testing.T) {
	f, fx := mixto()
	w, av := armarConFichas(t, f, fx)

	_ = w.Procesar(context.Background())
	antes := fx.total("u3")
	// Otra vuelta del worker y una liquidacion repetida a mano (p. ej. tras un
	// reinicio a mitad de mercado): Redis no paga dos veces.
	_ = w.Procesar(context.Background())
	if _, err := fx.LiquidarApuesta(context.Background(), "e1", "t-gana", true, 2.5); err != nil {
		t.Fatal(err)
	}

	if fx.total("u3") != antes || antes != 1300 || f.marcas != 2 || len(av.torneo["u3"]) != 1 {
		t.Errorf("fichas %d -> %d, marcas %d, avisos %v", antes, fx.total("u3"), f.marcas, av.torneo)
	}
}

func TestSinRedisLasDeTorneoQuedanPendientesYNoSeMarcaLiquidado(t *testing.T) {
	f, _ := mixto()
	w, av := armar(t, f) // Motor sin REDIS_URL: Fichas nil

	if err := w.Procesar(context.Background()); err == nil {
		t.Error("esperaba error: hay apuestas de torneo sin Redis")
	}
	if f.apuestas["b-gana"].Estado != "won" || f.apuestas["t-gana"].Estado != "pending" || f.mercados["m1"].Estado != "cerrado" {
		t.Errorf("individual %s, torneo %s, mercado %s", f.apuestas["b-gana"].Estado, f.apuestas["t-gana"].Estado, f.mercados["m1"].Estado)
	}
	if len(av.torneo) != 0 {
		t.Errorf("no debia avisar en torneo: %v", av.torneo)
	}
}

func TestRedisCaidoReintentaSinMarcarLost(t *testing.T) {
	f, fx := mixto()
	fx.falla = errors.New("dial tcp redis:6379: connection refused")
	w, av := armarConFichas(t, f, fx)

	if err := w.Procesar(context.Background()); err == nil {
		t.Error("esperaba error con Redis caido")
	}
	if f.apuestas["t-gana"].Estado != "pending" || f.apuestas["t-pierde"].Estado != "pending" || f.marcas != 0 || f.mercados["m1"].Estado != "cerrado" {
		t.Fatalf("con Redis caido nada de torneo se marca: %s %s marcas %d", f.apuestas["t-gana"].Estado, f.apuestas["t-pierde"].Estado, f.marcas)
	}

	fx.falla = nil // Redis vuelve
	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.apuestas["t-gana"].Estado != "won" || f.mercados["m1"].Estado != "liquidado" || len(av.torneo["u3"]) != 1 {
		t.Errorf("tras volver Redis: %s, mercado %s, avisos %v", f.apuestas["t-gana"].Estado, f.mercados["m1"].Estado, av.torneo)
	}
}

func TestApuestaDeTorneoSinReservaSeMarcaLostConCeroFichas(t *testing.T) {
	f, fx := mixto()
	delete(fx.reservas, "t-gana") // Pagos la registro pero la reserva nunca se hizo
	w, av := armarConFichas(t, f, fx)

	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a := f.apuestas["t-gana"]; a.Estado != "lost" || f.mercados["m1"].Estado != "liquidado" {
		t.Errorf("sin reserva: %s, mercado %s", a.Estado, f.mercados["m1"].Estado)
	}
	if fx.total("u3") != 1000 || len(av.torneo["u3"]) != 0 {
		t.Errorf("las fichas de u3 no cambian (%d) y no se le avisa (%v)", fx.total("u3"), av.torneo["u3"])
	}
}

func TestSiFallaLaMarcaEnPagosSeReintentaSinPagarFichasDosVeces(t *testing.T) {
	f, fx := mixto()
	f.fallarBet = "t-gana"
	w, av := armarConFichas(t, f, fx)

	_ = w.Procesar(context.Background())
	if f.apuestas["t-gana"].Estado != "pending" || f.mercados["m1"].Estado != "cerrado" || fx.total("u3") != 1300 {
		t.Fatalf("tras el fallo: %s, mercado %s, fichas %d", f.apuestas["t-gana"].Estado, f.mercados["m1"].Estado, fx.total("u3"))
	}
	f.fallarBet = ""
	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.apuestas["t-gana"].Estado != "won" || fx.total("u3") != 1300 || len(av.torneo["u3"]) != 1 || f.mercados["m1"].Estado != "liquidado" {
		t.Errorf("tras reintentar: %s, fichas %d, avisos %v", f.apuestas["t-gana"].Estado, fx.total("u3"), av.torneo["u3"])
	}
}

func TestTorneoCanceladoSoloIndividualesFuncionaConYSinRedis(t *testing.T) {
	// <4 inscritos: no hay fichas y el Motor apuesta todo en individual.
	for nombre, fx := range map[string]Fichas{"sin redis": nil, "con redis": nuevasFichas()} {
		f := nuevoPagos()
		w, _ := armarConFichas(t, f, fx)
		if err := w.Procesar(context.Background()); err != nil {
			t.Errorf("%s: %v", nombre, err)
		}
		if f.mercados["m1"].Estado != "liquidado" {
			t.Errorf("%s: mercado %s", nombre, f.mercados["m1"].Estado)
		}
		if ff, ok := fx.(*fichasFalsas); ok && ff.difusiones != 0 {
			t.Errorf("%s: sin apuestas de torneo no hace falta difundir el ranking", nombre)
		}
	}
}
