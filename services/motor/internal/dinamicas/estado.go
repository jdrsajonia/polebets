package dinamicas

import (
	"sort"
	"strconv"
)

// stint es lo que se sabe de un juego de llantas al empezar. No guarda
// lap_end: en el instante del evento todavia no ha pasado.
type stint struct {
	vueltaInicio int
	edadInicial  int
}

// Estado es lo que se sabe de la carrera en un instante, armado solo con los
// eventos ya ocurridos. Los mapas se consultan por clave; cuando hay que
// recorrerlos se ordenan primero (RNF-20).
type Estado struct {
	posiciones map[int]int      // piloto -> ultima posicion reportada
	intervalos map[int]*float64 // piloto -> ultimo intervalo (nil = sin dato)
	vueltas    map[int]int      // piloto -> ultima vuelta empezada
	stints     map[int][]stint  // piloto -> stints conocidos, por vuelta de inicio

	vueltaActual int // vuelta del lider: la mayor empezada por alguien
	liderVuelta  int // piloto que empezo primero la vuelta actual
	mejorVuelta  *float64

	codigos map[int]string // carreras.pilotos: numero -> codigo de 3 letras
}

// NuevoEstado arranca sin datos: antes de la largada todo es "sin dato".
// codigos son los codigos de 3 letras de carreras.pilotos (puede ser nil).
func NuevoEstado(codigos map[int]string) *Estado {
	return &Estado{
		codigos:    codigos,
		posiciones: map[int]int{},
		intervalos: map[int]*float64{},
		vueltas:    map[int]int{},
		stints:     map[int][]stint{},
	}
}

// Aplicar incorpora un evento. Las vueltas solo cuentan para la mejor vuelta
// cuando llega su TipoVueltaTerminada; el evento "vuelta" marca el inicio.
func (e *Estado) Aplicar(ev Evento) {
	switch ev.Tipo {
	case TipoPosicion:
		e.posiciones[ev.Piloto] = ev.Posicion
	case TipoIntervalo:
		e.intervalos[ev.Piloto] = ev.Intervalo
	case TipoVuelta:
		if ev.Vuelta > e.vueltas[ev.Piloto] {
			e.vueltas[ev.Piloto] = ev.Vuelta
		}
		if ev.Vuelta > e.vueltaActual {
			e.vueltaActual = ev.Vuelta
			e.liderVuelta = ev.Piloto
		}
	case TipoVueltaTerminada:
		if d := ev.DuracionVuelta; d != nil && *d > 0 && (e.mejorVuelta == nil || *d < *e.mejorVuelta) {
			v := *d
			e.mejorVuelta = &v
		}
	case TipoStint:
		lista := append(e.stints[ev.Piloto], stint{vueltaInicio: ev.VueltaInicio, edadInicial: ev.EdadInicial})
		sort.SliceStable(lista, func(i, j int) bool { return lista[i].vueltaInicio < lista[j].vueltaInicio })
		e.stints[ev.Piloto] = lista
	}
}

// Orden es la clasificacion actual: pilotos con posicion conocida, ordenados
// por (posicion, numero). El desempate cubre el instante en que dos pilotos
// reportan la misma posicion antes de que llegue el cambio del otro.
func (e *Estado) Orden() []int {
	orden := make([]int, 0, len(e.posiciones))
	for p := range e.posiciones {
		orden = append(orden, p)
	}
	sort.Slice(orden, func(i, j int) bool {
		pi, pj := e.posiciones[orden[i]], e.posiciones[orden[j]]
		if pi != pj {
			return pi < pj
		}
		return orden[i] < orden[j]
	})
	return orden
}

// Intervalo es el ultimo intervalo numerico del piloto al de adelante.
func (e *Estado) Intervalo(piloto int) (float64, bool) {
	v := e.intervalos[piloto]
	if v == nil {
		return 0, false
	}
	return *v, true
}

// VueltaActual es la vuelta del lider (0 antes de la largada).
func (e *Estado) VueltaActual() int { return e.vueltaActual }

// MejorVuelta es la vuelta mas rapida entre las ya terminadas.
func (e *Estado) MejorVuelta() (float64, bool) {
	if e.mejorVuelta == nil {
		return 0, false
	}
	return *e.mejorVuelta, true
}

// EdadLlantas = vuelta actual del piloto - lap_start + tyre_age_at_start, con
// el stint mas reciente que ya empezo para ese piloto. Un stint cuya vuelta
// de inicio el piloto aun no alcanza se ignora (el Ingestor lo fecha con la
// vuelta del lider, que puede ir adelante).
func (e *Estado) EdadLlantas(piloto int) (int, bool) {
	vuelta, ok := e.vueltas[piloto]
	if !ok {
		return 0, false
	}
	lista := e.stints[piloto]
	for i := len(lista) - 1; i >= 0; i-- {
		if s := lista[i]; s.vueltaInicio <= vuelta {
			return vuelta - s.vueltaInicio + s.edadInicial, true
		}
	}
	return 0, false
}

// Codigo es el codigo de 3 letras del piloto, o su numero si falta.
func (e *Estado) Codigo(piloto int) string {
	if c := e.codigos[piloto]; c != "" {
		return c
	}
	return strconv.Itoa(piloto)
}

// indice da la posicion (0 = lider) de un piloto en el orden, o -1.
func indice(orden []int, piloto int) int {
	for i, p := range orden {
		if p == piloto {
			return i
		}
	}
	return -1
}
