package main

import (
	"testing"
	"time"
)

var inicioPrueba = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)

// Regresión del bug de la segunda revisión: iniciada_en se tomaba de nuevo en
// cada tick y el reloj daba siempre ~0, así que nunca salía ningún evento.
func TestElRelojAvanzaDesdeIniciadaEn(t *testing.T) {
	r := nuevoRelojReproduccion(0, inicioPrueba)
	rg, ok := r.tick(inicioPrueba.Add(200 * time.Millisecond))
	if !ok || rg.Hasta != 200 {
		t.Fatalf("primer tick = %+v, %v; want Hasta 200", rg, ok)
	}
	r.confirmar(rg)
	for i := 2; i <= 50; i++ {
		ahora := inicioPrueba.Add(time.Duration(i) * 200 * time.Millisecond)
		rg, ok := r.tick(ahora)
		if !ok {
			t.Fatalf("tick %d: el reloj no avanzó", i)
		}
		if want := int64(i * 200); rg.Hasta != want || rg.Desde != want-200 {
			t.Fatalf("tick %d: rango %+v, want (%d, %d]", i, rg, want-200, want)
		}
		r.confirmar(rg)
	}
}

func TestTiempoCarreraConPuntoDeInicio(t *testing.T) {
	if got := tiempoCarrera(600000, inicioPrueba, inicioPrueba.Add(1500*time.Millisecond)); got != 601500 {
		t.Errorf("t = %d, want 601500", got)
	}
}

func TestElPrimerTickEmiteLoPrevioALaLargada(t *testing.T) {
	r := nuevoRelojReproduccion(0, inicioPrueba)
	rg, ok := r.tick(inicioPrueba.Add(150 * time.Millisecond))
	if !ok || !rg.SinDesde || rg.Hasta != 150 {
		t.Fatalf("primer tick = %+v, %v; want SinDesde y Hasta 150", rg, ok)
	}
	// Eventos con t ≤ 0 (clima, parrilla) y el de t = 0 entran en el primero.
	for _, ev := range []int64{-3600000, -1, 0, 150} {
		if !dentro(rg, ev) {
			t.Errorf("el evento en t=%d debería salir en el primer tick", ev)
		}
	}
	r.confirmar(rg)
	if rg2, _ := r.tick(inicioPrueba.Add(400 * time.Millisecond)); rg2.SinDesde || dentro(rg2, 0) {
		t.Errorf("el segundo tick no debe repetir lo previo: %+v", rg2)
	}
}

func TestSiElTickFallaNoSePierdenEventos(t *testing.T) {
	r := nuevoRelojReproduccion(0, inicioPrueba)
	rg, _ := r.tick(inicioPrueba.Add(200 * time.Millisecond))
	r.confirmar(rg)
	// Tick en 400 ms que falla al escribir: no se confirma.
	if _, ok := r.tick(inicioPrueba.Add(400 * time.Millisecond)); !ok {
		t.Fatal("debería avanzar")
	}
	rg, _ = r.tick(inicioPrueba.Add(600 * time.Millisecond))
	if rg.Desde != 200 || rg.Hasta != 600 {
		t.Errorf("tras un fallo el rango debe arrancar en lo último escrito: %+v", rg)
	}
}

// Cada evento sale exactamente una vez y en orden, aunque los ticks lleguen
// con atraso (RNF-20: la misma carrera da la misma secuencia).
func TestCadaEventoSaleUnaSolaVezYEnOrden(t *testing.T) {
	eventos := []int64{-5000, 0, 0, 120, 199, 200, 201, 950, 1000, 1001, 2999}
	visto := map[int]int{}
	var orden []int64
	r := nuevoRelojReproduccion(0, inicioPrueba)
	for _, ms := range []int{80, 300, 310, 900, 1700, 3000} {
		rg, ok := r.tick(inicioPrueba.Add(time.Duration(ms) * time.Millisecond))
		if !ok {
			continue
		}
		for i, ev := range eventos {
			if dentro(rg, ev) {
				visto[i]++
				orden = append(orden, ev)
			}
		}
		r.confirmar(rg)
	}
	for i := range eventos {
		if visto[i] != 1 {
			t.Errorf("evento %d (t=%d) salió %d veces", i, eventos[i], visto[i])
		}
	}
	for i := 1; i < len(orden); i++ {
		if orden[i] < orden[i-1] {
			t.Errorf("fuera de orden: %v", orden)
		}
	}
}

func TestElRelojNoRetrocede(t *testing.T) {
	r := nuevoRelojReproduccion(0, inicioPrueba)
	rg, _ := r.tick(inicioPrueba.Add(time.Second))
	r.confirmar(rg)
	if _, ok := r.tick(inicioPrueba.Add(900 * time.Millisecond)); ok {
		t.Error("un tick con hora anterior no debe emitir nada")
	}
}

func TestVueltaActualEsLaDelLider(t *testing.T) {
	ix := nuevoIndiceVueltas([]marcaVuelta{
		{T: 95000, Vuelta: 2}, {T: 0, Vuelta: 1}, {T: 0, Vuelta: 1},
		{T: 97000, Vuelta: 2}, {T: 190000, Vuelta: 3}, {T: 96000, Vuelta: 1}, // rezagado
	})
	casos := map[int64]int{-1: 0, 0: 1, 94999: 1, 95000: 2, 189999: 2, 190000: 3, 1 << 40: 3}
	for tms, want := range casos {
		if got := ix.vueltaActual(tms); got != want {
			t.Errorf("vueltaActual(%d) = %d, want %d", tms, got, want)
		}
	}
	if got := nuevoIndiceVueltas(nil).vueltaActual(1000); got != 0 {
		t.Errorf("sin vueltas = %d, want 0", got)
	}
}

func dentro(rg rangoTick, t int64) bool {
	return (rg.SinDesde || t > rg.Desde) && t <= rg.Hasta
}
