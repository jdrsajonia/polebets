package fichas

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/arquisoft/motor/internal/ws"
)

type hubFalso struct {
	mu   sync.Mutex
	msgs []ws.Mensaje
}

func (h *hubFalso) Difundir(m ws.Mensaje) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, m)
	return nil
}

func (h *hubFalso) eventos() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var r []string
	for _, m := range h.msgs {
		r = append(r, m.Payload.(Ranking).EventoID)
	}
	slices.Sort(r)
	return r
}

func TestReenviarDifundeElRankingDeCadaTorneoActivo(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	ctx := context.Background()
	for _, e := range []string{"evento-a", "evento-b"} {
		if err := a.InicializarTorneo(ctx, e, []string{"u1", "u2"}, 1000); err != nil {
			t.Fatalf("inicializar %s: %v", e, err)
		}
	}
	h := &hubFalso{}
	d := NuevoDifusor(a, h, ParametrosTorneo{FichasIniciales: 1000, MinWagerPct: 50})
	d.Pagos = &botinFalso{pot: map[string]int64{"evento-a": 400, "evento-b": 400}}

	d.Reenviar(ctx)
	if got := h.eventos(); !slices.Equal(got, []string{"evento-a", "evento-b"}) {
		t.Fatalf("difundidos = %v", got)
	}
	m := h.msgs[0]
	r := m.Payload.(Ranking)
	if m.Type != ws.TipoRankingTorneo || len(r.Filas) != 2 || r.MinApostado != 500 || r.Botin != 400 || r.EmitidoEn.IsZero() {
		t.Fatalf("mensaje = %+v", m)
	}

	// Cerrado (HU2-12 limpia Redis): deja de reenviarse.
	if err := a.Limpiar(ctx, "evento-a"); err != nil {
		t.Fatal(err)
	}
	h.msgs = nil
	d.Reenviar(ctx)
	if got := h.eventos(); !slices.Equal(got, []string{"evento-b"}) {
		t.Fatalf("difundidos tras cerrar evento-a = %v", got)
	}
}

func TestReenviarSinTorneosNoDifunde(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	h := &hubFalso{}
	NuevoDifusor(a, h, ParametrosTorneo{}).Reenviar(context.Background())
	if len(h.msgs) != 0 {
		t.Fatalf("difundio sin torneos: %v", h.msgs)
	}
}

func TestRunReenviaCadaIntervaloHastaCancelar(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	if err := a.InicializarTorneo(context.Background(), evt, []string{"u1"}, 1000); err != nil {
		t.Fatal(err)
	}
	h := &hubFalso{}
	d := NuevoDifusor(a, h, ParametrosTorneo{FichasIniciales: 1000})
	ctx, cancel := context.WithCancel(context.Background())
	listo := make(chan struct{})
	go func() {
		d.Run(ctx, 10*time.Millisecond)
		close(listo)
	}()
	time.Sleep(55 * time.Millisecond)
	cancel()
	<-listo
	n := len(h.eventos())
	if n < 3 {
		t.Fatalf("reenvios en ~55 ms cada 10 ms = %d", n)
	}
	time.Sleep(30 * time.Millisecond)
	if len(h.eventos()) != n {
		t.Fatal("siguio reenviando tras cancelar")
	}
}

// botinFalso devuelve el pot de cada evento, o los errores de su cola antes.
type botinFalso struct {
	mu      sync.Mutex
	pot     map[string]int64
	errores []error
	pedidos []string
}

func (b *botinFalso) Botin(_ context.Context, e string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pedidos = append(b.pedidos, e)
	if len(b.errores) > 0 {
		err := b.errores[0]
		b.errores = b.errores[1:]
		return 0, err
	}
	return b.pot[e], nil
}

func botinesDifundidos(h *hubFalso) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var r []int64
	for _, m := range h.msgs {
		r = append(r, m.Payload.(Ranking).Botin)
	}
	return r
}

func TestDifundirPideElBotinUnaVezPorEvento(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	ctx := context.Background()
	_ = a.InicializarTorneo(ctx, evt, []string{"u1"}, 1000)
	h := &hubFalso{}
	p := &botinFalso{pot: map[string]int64{evt: 1800}}
	d := NuevoDifusor(a, h, ParametrosTorneo{FichasIniciales: 1000})
	d.Pagos = p

	for i := 0; i < 3; i++ {
		d.Difundir(ctx, evt)
	}
	if got := botinesDifundidos(h); !slices.Equal(got, []int64{1800, 1800, 1800}) {
		t.Fatalf("botines = %v", got)
	}
	if len(p.pedidos) != 1 {
		t.Fatalf("pedidos a Pagos = %v, quiero 1", p.pedidos)
	}
}

func TestDifundirConPagosCaidoNoBloqueaYReintenta(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	ctx := context.Background()
	_ = a.InicializarTorneo(ctx, evt, []string{"u1"}, 1000)
	h := &hubFalso{}
	p := &botinFalso{pot: map[string]int64{evt: 1800}, errores: []error{ErrBotinNoDisponible, ErrBotinNoDisponible}}
	d := NuevoDifusor(a, h, ParametrosTorneo{FichasIniciales: 1000})
	d.Pagos = p

	for i := 0; i < 4; i++ {
		d.Difundir(ctx, evt)
	}
	// Las dos primeras salen con 0 (aun no se conoce); despues, el pot.
	if got := botinesDifundidos(h); !slices.Equal(got, []int64{0, 0, 1800, 1800}) {
		t.Fatalf("botines = %v", got)
	}
	if len(p.pedidos) != 3 {
		t.Fatalf("pedidos a Pagos = %v, quiero 3", p.pedidos)
	}
}

func TestReenviarOlvidaElBotinDeUnTorneoCerrado(t *testing.T) {
	a, _ := nuevoAlmacenDePrueba()
	ctx := context.Background()
	_ = a.InicializarTorneo(ctx, evt, []string{"u1"}, 1000)
	p := &botinFalso{pot: map[string]int64{evt: 1800}}
	d := NuevoDifusor(a, &hubFalso{}, ParametrosTorneo{FichasIniciales: 1000})
	d.Pagos = p
	d.Reenviar(ctx)
	_ = a.Limpiar(ctx, evt)
	d.Reenviar(ctx)
	if len(d.botines) != 0 {
		t.Fatalf("botines en memoria = %v", d.botines)
	}
}

func TestPagosBotinHTTP(t *testing.T) {
	const token = "token-interno-de-pruebas-del-motor-32c"
	var ruta, tokenRx string
	estado, cuerpo := http.StatusOK, `{"event_id":"e","pot":2400,"status":"en_curso"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ruta, tokenRx = r.Method+" "+r.URL.Path, r.Header.Get("X-Internal-Service-Token")
		w.WriteHeader(estado)
		_, _ = w.Write([]byte(cuerpo))
	}))
	defer srv.Close()
	p := NuevoPagosBotinHTTP(srv.URL+"/", token)

	b, err := p.Botin(context.Background(), "evento-1")
	if err != nil || b != 2400 {
		t.Fatalf("botin = %d, %v", b, err)
	}
	if ruta != "GET /internal/events/evento-1" || tokenRx != token {
		t.Fatalf("ruta = %q, token = %q", ruta, tokenRx)
	}

	for _, c := range []struct {
		estado int
		cuerpo string
	}{
		{http.StatusNotFound, `{"detail":"no existe"}`},
		{http.StatusServiceUnavailable, ``},
		{http.StatusOK, `{"event_id":"e"}`}, // sin pot
		{http.StatusOK, `no es json`},
	} {
		estado, cuerpo = c.estado, c.cuerpo
		if _, err := p.Botin(context.Background(), "evento-1"); !errors.Is(err, ErrBotinNoDisponible) {
			t.Errorf("%d %q: err = %v", c.estado, c.cuerpo, err)
		}
	}

	srv.Close()
	if _, err := p.Botin(context.Background(), "evento-1"); !errors.Is(err, ErrBotinNoDisponible) {
		t.Errorf("sin servidor: err = %v", err)
	}
}
