package dinamicas

import "strconv"

// Constructores de eventos para las pruebas. Los ids se arman con t y piloto
// para que el orden (t, _id) quede fijo.

func f(v float64) *float64 { return &v }

func id(t int64, tipo Tipo, piloto int) string {
	return strconv.FormatInt(t, 10) + "-" + string(tipo) + "-" + strconv.Itoa(piloto)
}

func pos(t int64, piloto, posicion int) Evento {
	return Evento{ID: id(t, TipoPosicion, piloto), T: t, Tipo: TipoPosicion, Piloto: piloto, Posicion: posicion}
}

func intv(t int64, piloto int, v *float64) Evento {
	return Evento{ID: id(t, TipoIntervalo, piloto), T: t, Tipo: TipoIntervalo, Piloto: piloto, Intervalo: v}
}

func vta(t int64, piloto, vuelta int, dur *float64) Evento {
	return Evento{ID: id(t, TipoVuelta, piloto), T: t, Tipo: TipoVuelta, Piloto: piloto, Vuelta: vuelta, DuracionVuelta: dur}
}

func stintEv(t int64, piloto, inicio, edad int) Evento {
	return Evento{ID: id(t, TipoStint, piloto), T: t, Tipo: TipoStint, Piloto: piloto, VueltaInicio: inicio, EdadInicial: edad}
}

func pitEv(t int64, piloto int) Evento {
	return Evento{ID: id(t, TipoPit, piloto), T: t, Tipo: TipoPit, Piloto: piloto}
}

func ctrl(t int64, bandera, mensaje string) Evento {
	return Evento{ID: id(t, TipoControl, 0), T: t, Tipo: TipoControl, Bandera: bandera, Mensaje: mensaje}
}

func terminada(t int64, piloto int, dur float64) Evento {
	return Evento{T: t, Tipo: TipoVueltaTerminada, Piloto: piloto, DuracionVuelta: &dur}
}

var codigos = map[int]string{1: "VER", 4: "NOR", 16: "LEC"}

func estadoCon(evs ...Evento) *Estado {
	e := NuevoEstado(codigos)
	for _, ev := range evs {
		e.Aplicar(ev)
	}
	return e
}

// parrilla: VER P1, NOR P2, LEC P3 en t.
func parrilla(t int64) []Evento {
	return []Evento{pos(t, 1, 1), pos(t, 4, 2), pos(t, 16, 3)}
}
