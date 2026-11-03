package cierre

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const evento = "e1"

// torneosFalsos imita el almacen: Cerrar reparte una vez (como Pagos con la
// clave event-close) y limpia Redis, asi el evento deja de estar activo.
type torneosFalsos struct {
	activos    map[string]bool
	orden      []string
	cierres    int // llamadas a Cerrar
	repartos   int // repartos que pagaron de verdad
	repartidos map[string]bool
	fallaCerr  error
	fallaLimp  bool // Pagos reparte pero la limpieza de Redis falla
}

func nuevosTorneos() *torneosFalsos {
	return &torneosFalsos{activos: map[string]bool{evento: true}, repartidos: map[string]bool{}}
}

func (t *torneosFalsos) EventosActivos(context.Context) ([]string, error) {
	var es []string
	for e, ok := range t.activos {
		if ok {
			es = append(es, e)
		}
	}
	return es, nil
}

func (t *torneosFalsos) DifundirRanking(_ context.Context, e string) {
	t.orden = append(t.orden, "ranking:"+e)
}

func (t *torneosFalsos) Cerrar(_ context.Context, e string) error {
	t.orden = append(t.orden, "cerrar:"+e)
	t.cierres++
	if t.fallaCerr != nil {
		return t.fallaCerr
	}
	if !t.repartidos[e] { // Pagos: la misma clave no paga dos veces
		t.repartidos[e] = true
		t.repartos++
	}
	if t.fallaLimp {
		return errors.New("reparto guardado en Pagos pero no se pudo limpiar Redis")
	}
	t.activos[e] = false
	return nil
}

type carreraFalsa struct {
	estado string
	ok     bool
	err    error
}

func (c *carreraFalsa) EstadoSimulacion(_ context.Context, carreraID string) (string, bool, error) {
	if carreraID != "2024_monza" {
		return "", false, nil
	}
	return c.estado, c.ok, c.err
}

// pagosFalso sirve GET /internal/events/{id} y /pendientes con el contrato real.
type pagosFalso struct {
	torneo     string
	pendientes Pendientes
	lecturas   int
}

func (f *pagosFalso) servidor(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Service-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.lecturas++
		switch r.URL.Path {
		case "/internal/events/" + evento:
			_ = json.NewEncoder(w).Encode(map[string]any{"event_id": evento, "race_key": "2024_monza", "tournament_status": f.torneo})
		case "/internal/events/" + evento + "/pendientes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"event_id": evento, "mercados_sin_liquidar": f.pendientes.Mercados, "apuestas_torneo_pending": f.pendientes.Torneo,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func armar(t *testing.T) (*Worker, *torneosFalsos, *pagosFalso, *carreraFalsa) {
	tf := nuevosTorneos()
	pf := &pagosFalso{torneo: "activo"}
	cf := &carreraFalsa{estado: SimulacionTerminada, ok: true}
	return NuevoWorker(tf, NuevoPagosHTTP(pf.servidor(t).URL, "tok"), cf, 0), tf, pf, cf
}

func TestNoCierraConPendientesNiConLaCarreraEnCurso(t *testing.T) {
	casos := map[string]func(*pagosFalso, *carreraFalsa){
		"carrera en curso":          func(_ *pagosFalso, c *carreraFalsa) { c.estado = "en_curso" },
		"carrera sin simular":       func(_ *pagosFalso, c *carreraFalsa) { c.ok = false },
		"mercados sin liquidar":     func(p *pagosFalso, _ *carreraFalsa) { p.pendientes.Mercados = 1 },
		"apuesta de torneo pending": func(p *pagosFalso, _ *carreraFalsa) { p.pendientes.Torneo = 2 },
		"torneo cancelado":          func(p *pagosFalso, _ *carreraFalsa) { p.torneo = TorneoCancelado },
	}
	for nombre, ajuste := range casos {
		w, tf, pf, cf := armar(t)
		ajuste(pf, cf)
		if err := w.Procesar(context.Background()); err != nil {
			t.Errorf("%s: %v", nombre, err)
		}
		if tf.cierres != 0 || len(tf.orden) != 0 {
			t.Errorf("%s: no debia cerrar ni difundir (%v)", nombre, tf.orden)
		}
	}
}

func TestCierraUnaVezCuandoNoQuedaNadaYDifundeAntes(t *testing.T) {
	w, tf, pf, _ := armar(t)
	pf.pendientes = Pendientes{Mercados: 1}
	_ = w.Procesar(context.Background())
	if tf.cierres != 0 {
		t.Fatal("cerro con un mercado sin liquidar")
	}

	pf.pendientes = Pendientes{} // HU2-10 y HU2-07 terminaron
	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tf.cierres != 1 || tf.repartos != 1 || len(tf.orden) != 2 || tf.orden[0] != "ranking:e1" || tf.orden[1] != "cerrar:e1" {
		t.Fatalf("cierres %d repartos %d orden %v", tf.cierres, tf.repartos, tf.orden)
	}
	// Otra vuelta: el evento ya no esta en Redis y ademas esta en memoria.
	_ = w.Procesar(context.Background())
	if tf.cierres != 1 {
		t.Errorf("cerro %d veces", tf.cierres)
	}
}

func TestRepetirTrasFalloNoPagaDoble(t *testing.T) {
	w, tf, _, _ := armar(t)

	tf.fallaLimp = true // Pagos repartio, Redis no se limpio
	if err := w.Procesar(context.Background()); err == nil {
		t.Fatal("esperaba error de limpieza")
	}
	tf.fallaLimp = false
	if err := w.Procesar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tf.cierres != 2 || tf.repartos != 1 || tf.activos[evento] {
		t.Errorf("cierres %d repartos %d activo %v", tf.cierres, tf.repartos, tf.activos[evento])
	}
}

func TestEventoYaCerradoEnMemoriaNoSeVuelveALlamar(t *testing.T) {
	w, tf, pf, _ := armar(t)
	_ = w.Procesar(context.Background())
	// Aunque Redis volviera a listarlo (p. ej. limpieza tardia), no se relee
	// Pagos ni se vuelve a cerrar.
	tf.activos[evento] = true
	antes := pf.lecturas
	_ = w.Procesar(context.Background())
	if tf.cierres != 1 || pf.lecturas != antes {
		t.Errorf("cierres %d, lecturas a Pagos %d -> %d", tf.cierres, antes, pf.lecturas)
	}
}

func TestErroresDejanElTorneoParaLaSiguienteVuelta(t *testing.T) {
	w, tf, _, cf := armar(t)
	cf.err = errors.New("mongo caido")
	if err := w.Procesar(context.Background()); err == nil || tf.cierres != 0 {
		t.Fatalf("con Mongo caido: err %v cierres %d", err, tf.cierres)
	}
	cf.err = nil
	tf.fallaCerr = errors.New("pagos no esta disponible")
	if err := w.Procesar(context.Background()); err == nil {
		t.Fatal("esperaba error de Pagos")
	}
	tf.fallaCerr = nil
	if err := w.Procesar(context.Background()); err != nil || tf.repartos != 1 {
		t.Errorf("tras recuperarse: err %v repartos %d", err, tf.repartos)
	}
}

func TestSinTokenNoCierra(t *testing.T) {
	tf := nuevosTorneos()
	pf := &pagosFalso{torneo: "activo"}
	w := NuevoWorker(tf, NuevoPagosHTTP(pf.servidor(t).URL, "otro"), &carreraFalsa{estado: SimulacionTerminada, ok: true}, 0)
	if err := w.Procesar(context.Background()); err == nil || tf.cierres != 0 {
		t.Errorf("err %v cierres %d", err, tf.cierres)
	}
}
