package fichas

import (
	"context"
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
	d := NuevoDifusor(a, h, ParametrosTorneo{FichasIniciales: 1000, MinWagerPct: 50, Botin: 400})

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
