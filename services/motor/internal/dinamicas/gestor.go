package dinamicas

import (
	"math"
	"sort"
	"strconv"
	"time"
)

// Reglas del Gestor (en milisegundos de carrera).
const (
	pausaTrasCierreMs        = 10_000  // la siguiente abre 10 s despues del cierre anterior
	cooldownPlantillaMs      = 180_000 // misma plantilla
	cooldownParMs            = 300_000 // mismo par de pilotos, en cualquier plantilla
	vueltaMinima             = 2       // no se abre antes de la vuelta 2 del lider
	vueltasFinalesSinMercado = 2       // ni en las 2 ultimas vueltas
)

// EstadoAbierto es el status con el que el Gestor crea los mercados. El
// worker decide si en una recuperacion lo crea "programado" (bloque 3).
const EstadoAbierto = "abierto"

// TipoAccion es lo que hay que hacer en Pagos.
type TipoAccion string

const (
	AccionCrear    TipoAccion = "crear"
	AccionCerrar   TipoAccion = "cerrar"
	AccionResolver TipoAccion = "resolver"
)

// Accion es una salida del Gestor, en el orden en que debe aplicarse.
type Accion struct {
	Tipo     TipoAccion
	T        int64   // tiempo de carrera en que ocurre
	Mercado  Mercado // copia del mercado en ese momento
	Estado   string  // crear: status del mercado
	Ganadora string  // resolver: opcion ganadora
}

// Config es lo que el Gestor necesita de la simulacion y de la carrera.
type Config struct {
	SimulacionID string
	// InicioMs es t_inicio_ms: el primer tick del Ingestor trae de golpe todo
	// lo anterior y con eso solo se arma el estado; no se abre nada antes.
	InicioMs     int64
	TotalVueltas int
	Pilotos      map[int]string // numero -> codigo de 3 letras
	Ventana      time.Duration  // config.VentanaApuestas
	Observacion  time.Duration  // config.VentanaObservacion
}

// porTerminar es una vuelta empezada que termina en el futuro.
type porTerminar struct {
	ev  Evento
	seq int
}

// Gestor es la maquina de estados de las dinamicas de una simulacion.
type Gestor struct {
	cfg        Config
	ventanaMs  int64
	obsMs      int64
	estado     *Estado
	plantillas []Plantilla

	abierto    *Mercado   // a lo sumo uno
	pendientes []*Mercado // cerrados sin resolver, por T0

	hayCierre       bool
	ultimoCierre    int64
	ultimaPlantilla map[string]int64 // plantilla -> T0 del ultimo mercado
	ultimoPar       map[[2]int]int64 // par de pilotos -> T0 del ultimo mercado

	vueltas []porTerminar // ordenadas por (T, seq)
	seq     int

	arrancado   bool
	reloj       int64
	terminado   bool
	descartados int
	acciones    []Accion
}

// NuevoGestor crea el Gestor de una simulacion.
func NuevoGestor(cfg Config) *Gestor {
	return &Gestor{
		cfg:             cfg,
		ventanaMs:       cfg.Ventana.Milliseconds(),
		obsMs:           cfg.Observacion.Milliseconds(),
		estado:          NuevoEstado(cfg.Pilotos),
		plantillas:      Catalogo,
		ultimaPlantilla: map[string]int64{},
		ultimoPar:       map[[2]int]int64{},
	}
}

// Descartados cuenta los eventos ignorados por llegar con t <= reloj anterior.
func (g *Gestor) Descartados() int { return g.descartados }

// Avanzar procesa los eventos (cursor, reloj], ordenados por (t, _id), y
// devuelve las acciones que generan. Dentro de la linea de tiempo:
//   - una vuelta que termina en x cuenta antes de los eventos con t >= x;
//   - un cierre o resolucion en x ocurre despues de los eventos con t == x
//     (el estado "en x" incluye todo lo que paso en x).
//
// Asi, partir la misma secuencia en lotes distintos da las mismas acciones.
func (g *Gestor) Avanzar(eventos []Evento, reloj int64) []Accion {
	if g.terminado {
		return nil
	}
	g.acciones = nil
	for _, ev := range eventos {
		if g.arrancado && ev.T <= g.reloj {
			g.descartados++
			continue
		}
		g.internos(ev.T, false)
		g.aplicar(ev, true)
		if ev.T > reloj {
			reloj = ev.T
		}
	}
	if !g.arrancado || reloj > g.reloj {
		g.reloj = reloj
	}
	g.arrancado = true
	g.internos(g.reloj, true)
	return g.acciones
}

// Terminar se llama cuando la simulacion finalizo o fallo, despues del
// ultimo Avanzar. Cierra el mercado abierto y devuelve los que quedan
// cerrados sin resolver (su observacion no alcanzo a terminar).
func (g *Gestor) Terminar() ([]Accion, []Mercado) {
	if g.terminado {
		return nil, nil
	}
	g.terminado = true
	g.acciones = nil
	if g.abierto != nil {
		g.cerrar(g.reloj)
	}
	sinResolver := make([]Mercado, 0, len(g.pendientes))
	for _, m := range g.pendientes {
		sinResolver = append(sinResolver, *m)
	}
	g.pendientes = nil
	return g.acciones, sinResolver
}

// internos ejecuta, en orden de tiempo, las vueltas que terminan hasta limite
// y los cierres/resoluciones antes de limite (o hasta limite si inclusivo).
func (g *Gestor) internos(limite int64, inclusivo bool) {
	for {
		tv := int64(math.MaxInt64)
		if len(g.vueltas) > 0 && g.vueltas[0].ev.T <= limite {
			tv = g.vueltas[0].ev.T
		}
		tt, esCierre, hay := g.proximoTimer()
		hay = hay && (tt < limite || (inclusivo && tt == limite))

		switch {
		case tv != math.MaxInt64 && (!hay || tv <= tt):
			ev := g.vueltas[0].ev
			g.vueltas = g.vueltas[1:]
			g.aplicar(ev, false)
		case hay && esCierre:
			g.cerrar(tt)
		case hay:
			g.resolver()
		default:
			return
		}
	}
}

// proximoTimer: con el mismo tiempo, la resolucion del mas antiguo va antes
// que el cierre del abierto.
func (g *Gestor) proximoTimer() (t int64, esCierre bool, hay bool) {
	if len(g.pendientes) > 0 {
		t, hay = g.pendientes[0].Resolucion, true
	}
	if g.abierto != nil && (!hay || g.abierto.Cierre < t) {
		t, esCierre, hay = g.abierto.Cierre, true, true
	}
	return t, esCierre, hay
}

// aplicar actualiza el estado, guarda el evento en las observaciones en curso
// y, si es un evento de Mongo, evalua las plantillas en su t.
func (g *Gestor) aplicar(ev Evento, externo bool) {
	g.estado.Aplicar(ev)
	if ev.Tipo == TipoVuelta && ev.DuracionVuelta != nil && *ev.DuracionVuelta > 0 {
		fin := ev
		fin.Tipo = TipoVueltaTerminada
		fin.T = ev.T + int64(math.Round(*ev.DuracionVuelta*1000))
		g.programar(fin)
	}
	for _, m := range g.pendientes {
		if ev.T > m.Cierre && ev.T <= m.Resolucion {
			m.obs = append(m.obs, ev)
		}
	}
	if externo {
		g.evaluar(ev)
	}
}

func (g *Gestor) programar(ev Evento) {
	g.seq++
	p := porTerminar{ev: ev, seq: g.seq}
	i := sort.Search(len(g.vueltas), func(i int) bool {
		v := g.vueltas[i]
		return v.ev.T > p.ev.T || (v.ev.T == p.ev.T && v.seq > p.seq)
	})
	g.vueltas = append(g.vueltas, porTerminar{})
	copy(g.vueltas[i+1:], g.vueltas[i:])
	g.vueltas[i] = p
}

// evaluar abre un mercado si se puede y alguna plantilla se cumple en ev.T.
func (g *Gestor) evaluar(ev Evento) {
	t := ev.T
	if g.abierto != nil || t < g.cfg.InicioMs {
		return
	}
	if g.hayCierre && t < g.ultimoCierre+pausaTrasCierreMs {
		return
	}
	v := g.estado.VueltaActual()
	if v < vueltaMinima || g.cfg.TotalVueltas <= 0 || v > g.cfg.TotalVueltas-vueltasFinalesSinMercado {
		return
	}
	for _, pl := range g.plantillas {
		if ultimo, ok := g.ultimaPlantilla[pl.ID()]; ok && t-ultimo < cooldownPlantillaMs {
			continue
		}
		for _, p := range pl.Evaluar(g.estado, ev) {
			if par, ok := clavePar(p.Pilotos); ok {
				if ultimo, ok := g.ultimoPar[par]; ok && t-ultimo < cooldownParMs {
					continue
				}
			}
			g.abrir(p, t)
			return
		}
	}
}

func (g *Gestor) abrir(p Propuesta, t0 int64) {
	m := &Mercado{
		Propuesta:  p,
		ID:         IDMercado(g.cfg.SimulacionID, p.Plantilla, t0),
		EventoID:   g.cfg.SimulacionID,
		T0:         t0,
		Cierre:     t0 + g.ventanaMs,
		Resolucion: t0 + g.ventanaMs + g.obsMs,
	}
	g.abierto = m
	g.ultimaPlantilla[p.Plantilla] = t0
	if par, ok := clavePar(p.Pilotos); ok {
		g.ultimoPar[par] = t0
	}
	g.acciones = append(g.acciones, Accion{Tipo: AccionCrear, T: t0, Mercado: *m, Estado: EstadoAbierto})
}

func (g *Gestor) cerrar(t int64) {
	m := g.abierto
	g.abierto = nil
	m.OrdenCierre = g.estado.Orden()
	if mv, ok := g.estado.MejorVuelta(); ok {
		m.MejorVueltaCierre = mv
	}
	g.hayCierre, g.ultimoCierre = true, t
	g.pendientes = append(g.pendientes, m)
	g.acciones = append(g.acciones, Accion{Tipo: AccionCerrar, T: t, Mercado: *m})
}

func (g *Gestor) resolver() {
	m := g.pendientes[0]
	g.pendientes = g.pendientes[1:]
	ganadora := OpcionNo
	for _, pl := range g.plantillas {
		if pl.ID() == m.Plantilla {
			ganadora = pl.Resolver(*m, g.estado, m.obs)
			break
		}
	}
	g.acciones = append(g.acciones, Accion{Tipo: AccionResolver, T: m.Resolucion, Mercado: *m, Ganadora: ganadora})
}

// clavePar normaliza el par para que (A, B) y (B, A) cuenten igual.
func clavePar(pilotos []int) ([2]int, bool) {
	if len(pilotos) != 2 {
		return [2]int{}, false
	}
	a, b := pilotos[0], pilotos[1]
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}, true
}

// IDMercado = UUIDv5(espacioMercados, simulacion|plantilla|t0).
func IDMercado(simulacionID, plantilla string, t0 int64) string {
	return uuidV5(espacioMercados, simulacionID+"|"+plantilla+"|"+strconv.FormatInt(t0, 10))
}
