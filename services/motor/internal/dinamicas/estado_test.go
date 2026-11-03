package dinamicas

import (
	"reflect"
	"testing"
)

func TestOrdenDesempataPorNumero(t *testing.T) {
	// LEC reporta P2 antes de que llegue el cambio de NOR: los dos en P2.
	e := estadoCon(append(parrilla(0), pos(10, 16, 2))...)
	if got := e.Orden(); !reflect.DeepEqual(got, []int{1, 4, 16}) {
		t.Fatalf("orden = %v", got)
	}
	e.Aplicar(pos(11, 4, 3))
	if got := e.Orden(); !reflect.DeepEqual(got, []int{1, 16, 4}) {
		t.Fatalf("orden = %v", got)
	}
}

func TestIntervaloSinDato(t *testing.T) {
	e := estadoCon(intv(0, 4, f(0.8)))
	if v, ok := e.Intervalo(4); !ok || v != 0.8 {
		t.Fatalf("intervalo = %v, %v", v, ok)
	}
	// Un null o "+1 LAP" posterior borra el dato anterior.
	e.Aplicar(intv(1, 4, nil))
	if _, ok := e.Intervalo(4); ok {
		t.Fatal("despues de null no hay dato")
	}
}

func TestVueltaActualYMejorVuelta(t *testing.T) {
	e := estadoCon(vta(0, 1, 1, nil), vta(500, 4, 1, nil), vta(90000, 1, 2, f(88)), vta(91000, 4, 2, f(89)))
	if e.VueltaActual() != 2 || e.liderVuelta != 1 {
		t.Fatalf("vuelta = %d, lider = %d", e.VueltaActual(), e.liderVuelta)
	}
	// El evento "vuelta" marca el inicio: la duracion no cuenta todavia.
	if _, ok := e.MejorVuelta(); ok {
		t.Fatal("ninguna vuelta ha terminado")
	}
	e.Aplicar(terminada(178000, 1, 88))
	e.Aplicar(terminada(180000, 4, 89))
	if m, ok := e.MejorVuelta(); !ok || m != 88 {
		t.Fatalf("mejor = %v", m)
	}
}

func TestEdadLlantas(t *testing.T) {
	casos := []struct {
		nombre string
		evs    []Evento
		edad   int
		ok     bool
	}{
		{"sin stint", []Evento{vta(0, 16, 5, nil)}, 0, false},
		{"sin vuelta", []Evento{stintEv(0, 16, 1, 0)}, 0, false},
		{"stint desde la largada", []Evento{stintEv(0, 16, 1, 0), vta(0, 16, 19, nil)}, 18, true},
		{"llantas usadas al salir", []Evento{stintEv(0, 16, 1, 3), vta(0, 16, 16, nil)}, 18, true},
		// El Ingestor fecha el stint nuevo con la vuelta del lider: si el
		// piloto aun no llega a lap_start, sigue con el stint anterior.
		{"stint futuro se ignora", []Evento{stintEv(0, 16, 1, 0), vta(0, 16, 20, nil), stintEv(1, 16, 21, 0)}, 19, true},
		{"stint nuevo ya empezado", []Evento{stintEv(0, 16, 1, 0), stintEv(1, 16, 21, 0), vta(2, 16, 23, nil)}, 2, true},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			edad, ok := estadoCon(c.evs...).EdadLlantas(16)
			if edad != c.edad || ok != c.ok {
				t.Fatalf("edad = %d, %v; se esperaba %d, %v", edad, ok, c.edad, c.ok)
			}
		})
	}
}

func TestCodigo(t *testing.T) {
	e := NuevoEstado(codigos)
	if e.Codigo(1) != "VER" || e.Codigo(99) != "99" {
		t.Fatalf("codigos = %s, %s", e.Codigo(1), e.Codigo(99))
	}
}
