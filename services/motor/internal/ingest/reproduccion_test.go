package ingest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Relanzar cancela la reproduccion anterior y espera a que termine del todo
// antes de iniciar el tablero de la nueva.
func TestRelanzarCancelaLaReproduccionAnterior(t *testing.T) {
	var activa reproduccionActiva
	var viejaTermino atomic.Bool
	viejaCorriendo := make(chan struct{})

	activa.lanzar(time.Minute, func() {}, func(ctx context.Context) {
		close(viejaCorriendo)
		<-ctx.Done()                      // solo sale si la cancelan
		time.Sleep(20 * time.Millisecond) // como detener el latido
		viejaTermino.Store(true)
	})
	<-viejaCorriendo

	var terminoAntesDeIniciar bool
	nuevaCtx := make(chan context.Context, 1)
	activa.lanzar(time.Minute,
		func() { terminoAntesDeIniciar = viejaTermino.Load() },
		func(ctx context.Context) { nuevaCtx <- ctx; <-ctx.Done() },
	)
	if !terminoAntesDeIniciar {
		t.Fatal("se inicio la reproduccion nueva antes de que terminara la vieja")
	}
	select {
	case ctx := <-nuevaCtx:
		if ctx.Err() != nil {
			t.Fatal("la reproduccion nueva no debe estar cancelada")
		}
	case <-time.After(time.Second):
		t.Fatal("la reproduccion nueva no arranco")
	}
}

// Dos relanzamientos a la vez: al final queda una sola reproduccion viva.
func TestRelanzamientosSimultaneosDejanUnaSola(t *testing.T) {
	var activa reproduccionActiva
	var vivas atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			arranco := make(chan struct{})
			activa.lanzar(time.Minute, func() {}, func(ctx context.Context) {
				vivas.Add(1)
				close(arranco)
				<-ctx.Done()
				vivas.Add(-1)
			})
			<-arranco
		}()
	}
	wg.Wait()
	// La ultima sigue viva; las demas se cancelaron y esperaron a salir.
	if n := vivas.Load(); n != 1 {
		t.Fatalf("reproducciones vivas = %d, quiero 1", n)
	}
}

type observadorFalso struct {
	mu      sync.Mutex
	cambios [][2]int
}

func (o *observadorFalso) Iniciar(string, []PilotoCarrera, map[int]int) {}

func (o *observadorFalso) Actualizar(numero, posicion int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cambios = append(o.cambios, [2]int{numero, posicion})
}

func evPos(numero, posicion int) EventoCarrera {
	return EventoCarrera{Tipo: "posicion", Datos: map[string]any{"driver_number": int32(numero), "position": int32(posicion)}}
}

func TestConsumirSoloAplicaPosiciones(t *testing.T) {
	eventos := make(chan EventoCarrera, 3)
	eventos <- evPos(4, 1)
	eventos <- EventoCarrera{Tipo: "bandera", Datos: map[string]any{"flag": "GREEN"}}
	eventos <- evPos(1, 2)
	close(eventos)

	obs := &observadorFalso{}
	consumir(context.Background(), eventos, obs)
	if len(obs.cambios) != 2 || obs.cambios[0] != [2]int{4, 1} || obs.cambios[1] != [2]int{1, 2} {
		t.Fatalf("cambios = %v", obs.cambios)
	}
}

// Lo que quedo en el buffer de una reproduccion cancelada no toca el tablero.
func TestConsumirCanceladoNoAplicaElBuffer(t *testing.T) {
	eventos := make(chan EventoCarrera, 2)
	eventos <- evPos(4, 1)
	eventos <- evPos(1, 2)
	close(eventos)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	obs := &observadorFalso{}
	consumir(ctx, eventos, obs)
	if len(obs.cambios) != 0 {
		t.Fatalf("aplico eventos de una reproduccion cancelada: %v", obs.cambios)
	}
}
