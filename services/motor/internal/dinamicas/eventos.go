// Package dinamicas implementa HU2-08: el Gestor de mercados dinamicos.
//
// Es la capa de dominio: no conoce Mongo, HTTP ni el hub. Recibe los eventos
// de la carrera ya normalizados y ordenados por (t_carrera_ms, _id), junto con
// el reloj de carrera, y devuelve acciones ordenadas para Pagos:
//
//	eventos (cursor, reloj] ──► Estado de carrera ──► 6 plantillas
//	                                                     │
//	Gestor.Avanzar(eventos, reloj) ◄─────────────────────┘
//	  └─► []Accion: Crear(mercado, abierto) · Cerrar(id) · Resolver(id, opcion)
//
// Todo se decide con el tiempo de carrera (RNF-19) y sin time.Now, rand ni
// recorridos de mapas sin ordenar (RNF-20): la misma carrera con el mismo
// punto de inicio produce los mismos mercados, con los mismos market_id,
// aunque los eventos lleguen en lotes distintos.
package dinamicas

import (
	"math"
	"strings"
)

// Tipo es el campo `tipo` de eventos_carrera. Solo se listan los que usa el
// estado; clima y ubicacion se ignoran.
type Tipo string

const (
	TipoPosicion  Tipo = "posicion"
	TipoIntervalo Tipo = "intervalo"
	TipoVuelta    Tipo = "vuelta"
	TipoStint     Tipo = "stint"
	TipoPit       Tipo = "pit"
	TipoControl   Tipo = "control"

	// TipoVueltaTerminada no viene de Mongo: el Gestor lo programa en
	// t + lap_duration, cuando la vuelta de verdad termina.
	TipoVueltaTerminada Tipo = "vuelta_terminada"
)

// TiposUsados son los tipos que el lector de Mongo debe traer.
var TiposUsados = []Tipo{TipoPosicion, TipoIntervalo, TipoVuelta, TipoStint, TipoPit, TipoControl}

// Evento es un dato de carrera normalizado. Solo se llenan los campos de su
// tipo. Los punteros nil significan "sin dato" (null o texto en OpenF1).
type Evento struct {
	ID     string // _id en Mongo; desempata el orden con el mismo T
	T      int64  // t_carrera_ms
	Tipo   Tipo
	Piloto int // driver_number

	Posicion    int      // posicion
	Intervalo   *float64 // intervalo: s al de adelante
	BrechaLider *float64 // intervalo: gap_to_leader

	Vuelta         int      // vuelta: lap_number
	DuracionVuelta *float64 // vuelta: lap_duration en s

	VueltaInicio int    // stint: lap_start
	EdadInicial  int    // stint: tyre_age_at_start
	Compuesto    string // stint: compound

	Categoria string // control: category
	Bandera   string // control: flag
	Mensaje   string // control: message
}

// Normalizar convierte la fila de OpenF1 (datos.raw) en un Evento. Devuelve
// false si el tipo no se usa o falta un campo obligatorio. A proposito no lee
// lap_end de los stints ni pit_duration de los pits: son datos del futuro en
// el instante del evento.
func Normalizar(tipo string, t int64, id string, raw map[string]any) (Evento, bool) {
	ev := Evento{ID: id, T: t, Tipo: Tipo(tipo)}
	switch ev.Tipo {
	case TipoControl:
		ev.Categoria = texto(raw["category"])
		ev.Bandera = strings.ToUpper(strings.TrimSpace(texto(raw["flag"])))
		ev.Mensaje = texto(raw["message"])
		return ev, true
	case TipoPosicion, TipoIntervalo, TipoVuelta, TipoStint, TipoPit:
	default:
		return Evento{}, false
	}

	piloto, ok := entero(raw["driver_number"])
	if !ok {
		return Evento{}, false
	}
	ev.Piloto = piloto

	switch ev.Tipo {
	case TipoPosicion:
		if ev.Posicion, ok = entero(raw["position"]); !ok || ev.Posicion <= 0 {
			return Evento{}, false
		}
	case TipoIntervalo:
		ev.Intervalo = opcional(raw["interval"])
		ev.BrechaLider = opcional(raw["gap_to_leader"])
	case TipoVuelta:
		if ev.Vuelta, ok = entero(raw["lap_number"]); !ok {
			return Evento{}, false
		}
		ev.DuracionVuelta = opcional(raw["lap_duration"])
	case TipoStint:
		if ev.VueltaInicio, ok = entero(raw["lap_start"]); !ok {
			return Evento{}, false
		}
		// null cuenta como llanta nueva.
		ev.EdadInicial, _ = entero(raw["tyre_age_at_start"])
		ev.Compuesto = texto(raw["compound"])
	}
	return ev, true
}

// numero lee un numero de bson (double, int32 o int64) o de json (float64).
func numero(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

func entero(v any) (int, bool) {
	f, ok := numero(v)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// opcional devuelve nil si el valor es null o texto (p. ej. "+1 LAP").
func opcional(v any) *float64 {
	f, ok := numero(v)
	if !ok {
		return nil
	}
	return &f
}

func texto(v any) string {
	s, _ := v.(string)
	return s
}
