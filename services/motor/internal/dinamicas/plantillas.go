package dinamicas

import (
	"fmt"
	"sort"
	"strings"
)

// Opciones de todos los mercados dinamicos.
const (
	OpcionSi = "Sí"
	OpcionNo = "No"
)

// Ids de las plantillas: van en markets.template y HU2-21 los usa en las
// consultas. No cambiarlos.
const (
	PlantillaAdelantamiento = "adelantamiento"
	PlantillaCambioLider    = "cambio_lider"
	PlantillaBrecha         = "brecha_se_cierra"
	PlantillaEntradaPits    = "entrada_pits"
	PlantillaVueltaRapida   = "vuelta_rapida"
	PlantillaSafetyCar      = "safety_car"
)

// Umbrales del catalogo (documentation/hu2-08-apuestas-dinamicas.md).
const (
	umbralAdelantamiento = 1.0 // s: B a menos de esto de A
	umbralCambioLider    = 1.5 // s: P2 a menos de esto del lider
	brechaMin            = 1.0 // s: B entre brechaMin y brechaMax de A
	brechaMax            = 3.0
	vueltasLlantas       = 18 // vueltas con las mismas llantas
	topPits              = 10
	cadaVueltas          = 10 // vuelta_rapida se evalua en 10, 20, 30...
)

// Opcion es una opcion del mercado con su multiplicador.
type Opcion struct {
	Nombre        string
	Multiplicador float64
}

// Propuesta es un mercado que una plantilla quiere abrir.
type Propuesta struct {
	Plantilla string
	Pregunta  string
	Opciones  []Opcion
	// Pilotos es el par [A, B] (A va delante) en adelantamiento y
	// brecha_se_cierra, y [L] en cambio_lider. Solo un par de dos activa el
	// cooldown por par.
	Pilotos []int
	// MejorVuelta es M al abrir (solo vuelta_rapida).
	MejorVuelta float64
}

// Plantilla es una regla del catalogo.
type Plantilla interface {
	ID() string
	// Evaluar mira el estado justo despues de aplicar ev (en ev.T) y devuelve
	// las propuestas en orden de preferencia; vacio = no se cumple.
	Evaluar(e *Estado, ev Evento) []Propuesta
	// Resolver decide la opcion ganadora con el estado en Resolucion y los
	// eventos de la observacion (Cierre, Resolucion].
	Resolver(m Mercado, e *Estado, obs []Evento) string
}

// Catalogo son las plantillas en orden de prioridad: si varias se cumplen en
// el mismo instante, gana la primera.
var Catalogo = []Plantilla{
	safetyCar{}, cambioLider{}, adelantamiento{}, entradaPits{}, brechaSeCierra{}, vueltaRapida{},
}

func siNo(si, no float64) []Opcion {
	return []Opcion{{Nombre: OpcionSi, Multiplicador: si}, {Nombre: OpcionNo, Multiplicador: no}}
}

func ganaSi(cond bool) string {
	if cond {
		return OpcionSi
	}
	return OpcionNo
}

// pareja es B con A justo delante y el intervalo de B.
type pareja struct {
	a, b      int
	posB      int // indice de B en el orden
	intervalo float64
}

// parejas devuelve las parejas con intervalo en [min, max) o [min, max]
// (segun hastaIncluido), ordenadas por intervalo y luego por posicion.
func parejas(e *Estado, min, max float64, hastaIncluido bool) []pareja {
	orden := e.Orden()
	var res []pareja
	for i := 1; i < len(orden); i++ {
		iv, ok := e.Intervalo(orden[i])
		if !ok || iv < min || iv > max || (!hastaIncluido && iv == max) {
			continue
		}
		res = append(res, pareja{a: orden[i-1], b: orden[i], posB: i, intervalo: iv})
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].intervalo != res[j].intervalo {
			return res[i].intervalo < res[j].intervalo
		}
		return res[i].posB < res[j].posB
	})
	return res
}

// delante dice si x va delante de y en el orden actual.
func delante(e *Estado, x, y int) bool {
	orden := e.Orden()
	ix, iy := indice(orden, x), indice(orden, y)
	return ix >= 0 && iy >= 0 && ix < iy
}

// 1. adelantamiento
type adelantamiento struct{}

func (adelantamiento) ID() string { return PlantillaAdelantamiento }

func (adelantamiento) Evaluar(e *Estado, _ Evento) []Propuesta {
	var res []Propuesta
	for _, p := range parejas(e, 0, umbralAdelantamiento, false) {
		res = append(res, Propuesta{
			Plantilla: PlantillaAdelantamiento,
			Pregunta:  fmt.Sprintf("¿%s irá delante de %s al terminar el próximo minuto?", e.Codigo(p.b), e.Codigo(p.a)),
			Opciones:  siNo(3.0, 1.3),
			Pilotos:   []int{p.a, p.b},
		})
	}
	return res
}

func (adelantamiento) Resolver(m Mercado, e *Estado, _ []Evento) string {
	a, b := m.Pilotos[0], m.Pilotos[1]
	return ganaSi(delante(e, b, a))
}

// 2. cambio_lider
type cambioLider struct{}

func (cambioLider) ID() string { return PlantillaCambioLider }

func (cambioLider) Evaluar(e *Estado, _ Evento) []Propuesta {
	orden := e.Orden()
	if len(orden) < 2 {
		return nil
	}
	lider := orden[0]
	iv, ok := e.Intervalo(orden[1])
	if !ok || iv < 0 || iv >= umbralCambioLider {
		return nil
	}
	return []Propuesta{{
		Plantilla: PlantillaCambioLider,
		Pregunta:  fmt.Sprintf("¿Habrá un líder distinto de %s al terminar el próximo minuto?", e.Codigo(lider)),
		Opciones:  siNo(3.5, 1.25),
		Pilotos:   []int{lider},
	}}
}

func (cambioLider) Resolver(m Mercado, e *Estado, _ []Evento) string {
	orden := e.Orden()
	return ganaSi(len(orden) > 0 && orden[0] != m.Pilotos[0])
}

// 3. brecha_se_cierra
type brechaSeCierra struct{}

func (brechaSeCierra) ID() string { return PlantillaBrecha }

func (brechaSeCierra) Evaluar(e *Estado, _ Evento) []Propuesta {
	var res []Propuesta
	for _, p := range parejas(e, brechaMin, brechaMax, true) {
		res = append(res, Propuesta{
			Plantilla: PlantillaBrecha,
			Pregunta:  fmt.Sprintf("¿%s estará a menos de 1 s de %s al terminar el próximo minuto?", e.Codigo(p.b), e.Codigo(p.a)),
			Opciones:  siNo(2.5, 1.5),
			Pilotos:   []int{p.a, p.b},
		})
	}
	return res
}

// Gana Si si B esta delante de A, o detras de A con su ultimo intervalo < 1 s.
func (brechaSeCierra) Resolver(m Mercado, e *Estado, _ []Evento) string {
	a, b := m.Pilotos[0], m.Pilotos[1]
	if delante(e, b, a) {
		return OpcionSi
	}
	iv, ok := e.Intervalo(b)
	return ganaSi(delante(e, a, b) && ok && iv >= 0 && iv < brechaMin)
}

// 4. entrada_pits
type entradaPits struct{}

func (entradaPits) ID() string { return PlantillaEntradaPits }

func (entradaPits) Evaluar(e *Estado, _ Evento) []Propuesta {
	for _, p := range top(e.Orden(), topPits) {
		if edad, ok := e.EdadLlantas(p); ok && edad >= vueltasLlantas {
			return []Propuesta{{
				Plantilla: PlantillaEntradaPits,
				Pregunta:  "¿Algún piloto del top 10 entrará a pits en el próximo minuto?",
				Opciones:  siNo(2.2, 1.6),
			}}
		}
	}
	return nil
}

// Gana Si si hay un pit en la observacion de alguien del top 10 en el cierre.
func (entradaPits) Resolver(m Mercado, _ *Estado, obs []Evento) string {
	top10 := top(m.OrdenCierre, topPits)
	for _, ev := range obs {
		if ev.Tipo == TipoPit && indice(top10, ev.Piloto) >= 0 {
			return OpcionSi
		}
	}
	return OpcionNo
}

func top(orden []int, n int) []int {
	if len(orden) < n {
		return orden
	}
	return orden[:n]
}

// 5. vuelta_rapida
type vueltaRapida struct{}

func (vueltaRapida) ID() string { return PlantillaVueltaRapida }

// Solo en el evento con el que el lider empieza la vuelta 10, 20, 30...
func (vueltaRapida) Evaluar(e *Estado, ev Evento) []Propuesta {
	if ev.Tipo != TipoVuelta || ev.Vuelta <= 0 || ev.Vuelta%cadaVueltas != 0 ||
		ev.Vuelta != e.VueltaActual() || ev.Piloto != e.liderVuelta {
		return nil
	}
	m, ok := e.MejorVuelta()
	if !ok {
		return nil
	}
	return []Propuesta{{
		Plantilla:   PlantillaVueltaRapida,
		Pregunta:    "¿Habrá una nueva vuelta rápida de la carrera en el próximo minuto?",
		Opciones:    siNo(2.8, 1.35),
		MejorVuelta: m,
	}}
}

// Gana Si si una vuelta que termina en la observacion baja M.
func (vueltaRapida) Resolver(m Mercado, _ *Estado, obs []Evento) string {
	for _, ev := range obs {
		if ev.Tipo == TipoVueltaTerminada && ev.DuracionVuelta != nil && *ev.DuracionVuelta > 0 && *ev.DuracionVuelta < m.MejorVuelta {
			return OpcionSi
		}
	}
	return OpcionNo
}

// 6. safety_car
type safetyCar struct{}

func (safetyCar) ID() string { return PlantillaSafetyCar }

func (safetyCar) Evaluar(_ *Estado, ev Evento) []Propuesta {
	if ev.Tipo != TipoControl || (ev.Bandera != "YELLOW" && ev.Bandera != "DOUBLE YELLOW") {
		return nil
	}
	return []Propuesta{{
		Plantilla: PlantillaSafetyCar,
		Pregunta:  "¿Saldrá el Safety Car o el VSC en el próximo minuto?",
		Opciones:  siNo(4.0, 1.2),
	}}
}

// "SAFETY CAR DEPLOYED" tambien cubre "VIRTUAL SAFETY CAR DEPLOYED".
func (safetyCar) Resolver(_ Mercado, _ *Estado, obs []Evento) string {
	for _, ev := range obs {
		if ev.Tipo == TipoControl && strings.Contains(strings.ToUpper(ev.Mensaje), "SAFETY CAR DEPLOYED") {
			return OpcionSi
		}
	}
	return OpcionNo
}
