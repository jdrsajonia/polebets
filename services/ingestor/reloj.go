package main

import (
	"sort"
	"time"
)

// tiempoCarrera es el reloj de carrera de una simulación:
// t = t_inicio_ms + (ahora − iniciada_en), en milisegundos.
func tiempoCarrera(tInicioMs int64, iniciadaEn, ahora time.Time) int64 {
	return tInicioMs + ahora.Sub(iniciadaEn).Milliseconds()
}

// rangoTick son los eventos que salen en un tick: t_carrera_ms en (Desde, Hasta].
// SinDesde marca el primer tick, que emite todo lo que hay hasta Hasta,
// incluidos los datos previos a la largada (t ≤ 0: clima, parrilla, control).
type rangoTick struct {
	SinDesde bool
	Desde    int64
	Hasta    int64
}

// relojReproduccion lleva el avance de una reproducción. iniciadaEn es fija (la
// que se guardó al pasar la simulación a en_curso) y tAnterior solo avanza
// cuando el tick se escribió bien, así un fallo de Mongo se recupera en el
// siguiente tick sin perder eventos.
type relojReproduccion struct {
	tInicio    int64
	iniciadaEn time.Time
	tAnterior  int64
	primero    bool
}

func nuevoRelojReproduccion(tInicioMs int64, iniciadaEn time.Time) *relojReproduccion {
	return &relojReproduccion{tInicio: tInicioMs, iniciadaEn: iniciadaEn, tAnterior: tInicioMs, primero: true}
}

// tick devuelve el rango a emitir en `ahora`, o false si el reloj no avanzó.
func (r *relojReproduccion) tick(ahora time.Time) (rangoTick, bool) {
	t := tiempoCarrera(r.tInicio, r.iniciadaEn, ahora)
	if r.primero {
		return rangoTick{SinDesde: true, Hasta: t}, true
	}
	if t <= r.tAnterior {
		return rangoTick{}, false
	}
	return rangoTick{Desde: r.tAnterior, Hasta: t}, true
}

// confirmar registra que el rango ya se escribió en Mongo.
func (r *relojReproduccion) confirmar(rg rangoTick) {
	r.tAnterior = rg.Hasta
	r.primero = false
}

// marcaVuelta es el arranque de una vuelta de un piloto (evento tipo "vuelta").
type marcaVuelta struct {
	T      int64
	Vuelta int
}

// indiceVueltas responde la vuelta del líder en un tiempo de carrera.
type indiceVueltas struct {
	ts       []int64
	maxHasta []int // mayor vuelta empezada hasta ts[i], inclusive
}

func nuevoIndiceVueltas(marcas []marcaVuelta) indiceVueltas {
	ordenadas := append([]marcaVuelta(nil), marcas...)
	sort.Slice(ordenadas, func(i, j int) bool { return ordenadas[i].T < ordenadas[j].T })
	ix := indiceVueltas{ts: make([]int64, len(ordenadas)), maxHasta: make([]int, len(ordenadas))}
	maximo := 0
	for i, m := range ordenadas {
		if m.Vuelta > maximo {
			maximo = m.Vuelta
		}
		ix.ts[i] = m.T
		ix.maxHasta[i] = maximo
	}
	return ix
}

// vueltaActual es el mayor lap_number con t_carrera_ms ≤ t (0 antes de la
// largada), sin pasar de total (la vuelta de enfriamiento no se muestra).
// total ≤ 0 significa que no se conoce.
func (ix indiceVueltas) vueltaActual(t int64, total int) int {
	i := sort.Search(len(ix.ts), func(i int) bool { return ix.ts[i] > t })
	if i == 0 {
		return 0
	}
	if v := ix.maxHasta[i-1]; total <= 0 || v < total {
		return v
	}
	return total
}
