package estaticas

// EventoPosicion es un dato de posicion de la carrera, ya normalizado desde
// eventos_carrera (tipo "posicion"): a que t_carrera_ms, que piloto y en que
// posicion iba. El paquete de dominio no conoce Mongo; el lector lo arma.
type EventoPosicion struct {
	T      int64 // t_carrera_ms
	Piloto int   // driver_number
	Pos    int   // position (1 = lider)
}

// ganador devuelve el codigo del piloto que termino primero: aquel cuyo
// ULTIMO evento de posicion (mayor t_carrera_ms) vale position == 1. Los
// eventos son incrementales, asi que la ultima foto de cada piloto manda.
//
// pilotos mapea driver_number -> codigo de 3 letras (para casar con la opcion
// del mercado). Devuelve ("", false) si no hay un lider claro al final o si
// el piloto lider no tiene codigo conocido.
func ganador(posiciones []EventoPosicion, pilotos map[int]string) (string, bool) {
	// Ultima posicion conocida de cada piloto (el evento de mayor t).
	ultimaPos := make(map[int]int, len(pilotos))
	ultimoT := make(map[int]int64, len(pilotos))
	visto := false
	for _, e := range posiciones {
		if e.Pos <= 0 {
			continue
		}
		if t, ok := ultimoT[e.Piloto]; !ok || e.T >= t {
			ultimoT[e.Piloto] = e.T
			ultimaPos[e.Piloto] = e.Pos
			visto = true
		}
	}
	if !visto {
		return "", false
	}

	for piloto, pos := range ultimaPos {
		if pos == 1 {
			codigo, ok := pilotos[piloto]
			if !ok || codigo == "" {
				return "", false
			}
			return codigo, true
		}
	}
	return "", false
}
