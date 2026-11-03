package estaticas

import (
	"sort"
)

// Catalogo estatico del prototipo. Por ahora una sola plantilla solida
// ("ganador"); anadir mas es sumar entradas aqui, sin tocar el worker.
const (
	// PlantillaGanador: "¿Quien gana la carrera?". Opciones = los pilotos.
	PlantillaGanador = "ganador"
	PreguntaGanador  = "¿Quién gana la carrera?"

	// KindEstatica es markets.kind de los mercados de HU2-07.
	KindEstatica = "estatica"
)

// Multiplicadores por tramo, segun el nivel del piloto (clasificacion F1
// 2024). Los fija el Motor: OpenF1 no da cuotas. El favorito paga poco; un
// piloto fuera de la tabla (o de otra temporada) cae en el multiplicador por
// defecto, mas alto.
const multiplicadorPorDefecto = 15.0

var multiplicadorPorCodigo = map[string]float64{
	// Top
	"VER": 3.0, "NOR": 3.0, "LEC": 3.0, "PIA": 3.0,
	// Alto
	"SAI": 6.0, "RUS": 6.0, "HAM": 6.0,
	// Medio
	"PER": 10.0, "ALO": 10.0, "GAS": 10.0, "HUL": 10.0,
}

// multiplicadorDe devuelve el multiplicador del piloto por su codigo de 3
// letras, o el por defecto si no esta en la tabla.
func multiplicadorDe(codigo string) float64 {
	if m, ok := multiplicadorPorCodigo[codigo]; ok {
		return m
	}
	return multiplicadorPorDefecto
}

// Opcion es una opcion de un mercado estatico: el codigo del piloto y su
// multiplicador.
type Opcion struct {
	Nombre        string
	Multiplicador float64
}

// opcionesGanador arma las opciones del mercado "ganador" a partir de los
// pilotos de la carrera (numero -> codigo). Ordena por codigo para que el
// mismo conjunto de pilotos de siempre el mismo mercado (RNF-20). Un piloto
// sin codigo se omite: su opcion no podria mostrarse ni casar con el ganador.
func opcionesGanador(pilotos map[int]string) []Opcion {
	codigos := make([]string, 0, len(pilotos))
	vistos := make(map[string]bool, len(pilotos))
	for _, c := range pilotos {
		if c == "" || vistos[c] {
			continue
		}
		vistos[c] = true
		codigos = append(codigos, c)
	}
	sort.Strings(codigos)

	opciones := make([]Opcion, 0, len(codigos))
	for _, c := range codigos {
		opciones = append(opciones, Opcion{Nombre: c, Multiplicador: multiplicadorDe(c)})
	}
	return opciones
}

// IDMercado = UUIDv5(espacioMercados, event_id|estatica|plantilla). Depende
// solo del evento y la plantilla: un reinicio deriva el mismo id y Pagos
// responde 200 a lo ya creado (idempotencia, RNF-12).
func IDMercado(eventoID, plantilla string) string {
	return uuidV5(espacioMercados, eventoID+"|"+KindEstatica+"|"+plantilla)
}
