package dinamicas

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"
)

// TipoMercadoDinamico es el mensaje WebSocket (mismo valor que
// ws.TipoMercadoDinamico; este paquete no importa ws para probarse solo).
const TipoMercadoDinamico = "mercado_dinamico"

// EstadoProgramado es el status con el que se crea, en una recuperacion, un
// mercado cuyo race_close ya paso: nunca queda abierto y no entra ninguna
// apuesta tarde. Despues viene su Cerrar.
const EstadoProgramado = "programado"

// avisoReintento: el aviso de una accion que queda para el siguiente tick
// sale a lo sumo una vez por este tiempo y por market_id (el tick es de
// 500 ms; sin esto, un evento que aun no existe en Pagos llena el log).
const avisoReintento = 10 * time.Second

// margenVivoMs: en el primer lote de una simulacion, una accion con t mas
// atras que esto respecto al reloj es de recuperacion y no se difunde.
const margenVivoMs = 5_000

// Difusor manda un mensaje a todos los conectados (el Hub, adaptado en
// main.go). nil = no se difunde.
type Difusor interface {
	Difundir(tipo string, payload any) error
}

// MensajeDinamico es el payload de "mercado_dinamico".
type MensajeDinamico struct {
	MarketID   string        `json:"market_id"`
	EventID    string        `json:"event_id"`
	Template   string        `json:"template"`
	Question   string        `json:"question"`
	Options    []opcionPagos `json:"options"`
	Status     string        `json:"status"` // abierto | cerrado
	RaceCloseS int64         `json:"race_close_s"`
	// ClosesAt es el mismo closes_at que va a Pagos (Simulacion.HoraReal del
	// cierre), en UTC: el front lo usa para la cuenta regresiva.
	ClosesAt      time.Time `json:"closes_at"`
	WinningOption *string   `json:"winning_option"` // solo al resolver
}

// pendiente es una accion del Gestor que aun no se aplico en Pagos.
type pendiente struct {
	Accion
	vivo bool // se difunde por WebSocket
}

// Worker lleva el Gestor de la simulacion actual: lee Mongo, avanza el Gestor
// y aplica sus acciones en Pagos, en orden.
//
//   - Simulacion en_curso: una accion que falla de forma reintentable queda
//     en la cola y se reintenta en el siguiente tick; mientras haya cola no se
//     leen eventos nuevos, asi nada se duplica. Un 409 u otro rechazo se
//     loguea y se salta (reintentarlo no lo arregla).
//   - Simulacion ya terminada (finalizada o fallida): se procesa UNA vez para
//     recuperar lo que quedo a medias; si una accion falla se loguea y se
//     abandona la simulacion hasta que aparezca otra.
//   - Al arrancar a mitad de carrera se reprocesa desde el inicio: todo es
//     determinista e idempotente y Pagos responde 200 a lo ya creado.
type Worker struct {
	Fuente      Fuente
	Pagos       Pagos
	Difusor     Difusor
	Intervalo   time.Duration
	Ventana     time.Duration
	Observacion time.Duration

	sim        Simulacion
	gestor     *Gestor
	cursor     int64
	primerLote bool
	terminado  bool // ya se llamo Terminar en el Gestor
	cola       []pendiente
	abandonada string

	// Ahora es la hora real, solo para espaciar los logs (nunca decide
	// mercados: eso es del tiempo de carrera). nil = time.Now.
	Ahora    func() time.Time
	avisados map[string]time.Time // market_id -> ultimo aviso de reintento
}

func NuevoWorker(f Fuente, p Pagos, d Difusor, ventana, observacion time.Duration) *Worker {
	return &Worker{Fuente: f, Pagos: p, Difusor: d, Intervalo: 500 * time.Millisecond, Ventana: ventana, Observacion: observacion}
}

// Run procesa cada Intervalo hasta que se cancele el contexto.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Intervalo)
	defer ticker.Stop()
	for {
		if err := w.Procesar(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-08: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Procesar hace un tick.
func (w *Worker) Procesar(ctx context.Context) error {
	sim, ok, err := w.Fuente.SimulacionActual(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if sim.ID != w.sim.ID {
		w.cambiar(ctx, sim)
	}
	w.sim = sim
	if sim.ID == w.abandonada {
		return nil
	}
	if w.gestor == nil {
		if sim.TCarreraMs == math.MinInt64 {
			return nil // en_curso que aun no late (o fallida sin haber largado)
		}
		datos, err := w.Fuente.Carrera(ctx, sim.CarreraID)
		if err != nil {
			return err // se reintenta en el siguiente tick sin crear el Gestor
		}
		w.gestor = NuevoGestor(Config{
			SimulacionID: sim.ID,
			InicioMs:     sim.TInicioMs,
			TotalVueltas: datos.TotalVueltas,
			Pilotos:      datos.Pilotos,
			Ventana:      w.Ventana,
			Observacion:  w.Observacion,
		})
		w.cursor, w.primerLote = math.MinInt64, true
		log.Printf("HU2-08: simulacion %s (carrera %s, %s): procesando desde el inicio hasta t=%s",
			sim.ID, sim.CarreraID, sim.Estado, seg(sim.TCarreraMs))
	}

	if w.aplicarCola(ctx); len(w.cola) > 0 || w.terminado {
		return nil
	}

	reloj := sim.TCarreraMs
	var acciones []Accion
	if reloj > w.cursor {
		evs, err := w.Fuente.Eventos(ctx, sim.CarreraID, w.cursor, reloj)
		if err != nil {
			return err
		}
		acciones = w.gestor.Avanzar(evs, reloj)
		w.cursor = reloj
		if n := w.gestor.Descartados(); n > 0 {
			log.Printf("HU2-08: ATENCION %d eventos llegaron con t ya procesado y se ignoraron", n)
		}
	}
	if sim.Termino() {
		fin, sinResolver := w.gestor.Terminar()
		acciones = append(acciones, fin...)
		w.terminado = true
		logSinResolver(sim, sinResolver)
	}
	w.encolar(acciones, reloj, sim.Termino())
	w.aplicarCola(ctx)
	return nil
}

// encolar marca que se difunde: en el primer lote (al arrancar el Gestor) lo
// que esta lejos del reloj es recuperacion, y en una simulacion que ya
// estaba terminada nada se difunde.
func (w *Worker) encolar(acciones []Accion, reloj int64, termino bool) {
	for _, a := range acciones {
		recuperacion := w.primerLote && (termino || reloj-a.T > margenVivoMs)
		w.cola = append(w.cola, pendiente{Accion: a, vivo: !recuperacion})
	}
	w.primerLote = false
}

// cambiar cierra lo que dejo la simulacion anterior (Terminar en su Gestor)
// antes de pasar a la nueva. Se intenta una vez: la anterior ya no avanza.
func (w *Worker) cambiar(ctx context.Context, nueva Simulacion) {
	if w.gestor != nil && w.sim.ID != w.abandonada {
		if !w.terminado {
			fin, sinResolver := w.gestor.Terminar()
			w.encolar(fin, w.sim.TCarreraMs, false)
			logSinResolver(w.sim, sinResolver)
		}
		if len(w.cola) > 0 {
			log.Printf("HU2-08: simulacion %s reemplazada por %s: aplicando %d acciones pendientes", w.sim.ID, nueva.ID, len(w.cola))
			w.sim.Estado = SimFallida // se aplica como terminada: un intento, sin bucle
			w.aplicarCola(ctx)
		}
	}
	w.gestor, w.cola, w.terminado, w.abandonada, w.avisados = nil, nil, false, "", nil
}

// aplicarCola aplica en orden. Una accion reintentable de una simulacion
// en_curso se queda al frente de la cola para el siguiente tick.
func (w *Worker) aplicarCola(ctx context.Context) {
	for len(w.cola) > 0 {
		p := w.cola[0]
		err := w.aplicar(ctx, p)
		if err == nil {
			w.cola = w.cola[1:]
			delete(w.avisados, p.Mercado.ID)
			continue
		}
		if w.sim.Termino() {
			log.Printf("HU2-08: ATENCION simulacion %s terminada: %s fallo (%v); se abandona con %d acciones sin aplicar",
				w.sim.ID, describir(p.Accion), err, len(w.cola))
			w.cola, w.abandonada = nil, w.sim.ID
			return
		}
		if errors.Is(err, ErrReintentable) {
			w.avisarReintento(p.Accion, err)
			return
		}
		log.Printf("HU2-08: ATENCION %s no se reintenta: %v", describir(p.Accion), err)
		w.cola = w.cola[1:]
	}
}

// avisarReintento loguea a lo sumo una vez cada avisoReintento por mercado.
func (w *Worker) avisarReintento(a Accion, err error) {
	ahora := time.Now()
	if w.Ahora != nil {
		ahora = w.Ahora()
	}
	if previo, ok := w.avisados[a.Mercado.ID]; ok && ahora.Sub(previo) < avisoReintento {
		return
	}
	if w.avisados == nil {
		w.avisados = map[string]time.Time{}
	}
	w.avisados[a.Mercado.ID] = ahora
	log.Printf("HU2-08: %s queda para el siguiente tick: %v (se reintenta cada tick; este aviso sale cada %s)",
		describir(a), err, avisoReintento)
}

func (w *Worker) aplicar(ctx context.Context, p pendiente) error {
	m := p.Mercado
	estado := "cerrado"
	var ganadora *string
	switch p.Tipo {
	case AccionCrear:
		estado = p.Estado
		if w.sim.TCarreraMs >= m.Cierre {
			estado = EstadoProgramado
		}
		nuevo, err := w.Pagos.Crear(ctx, Alta{
			Mercado: m, Estado: estado,
			AbreEn: w.sim.HoraReal(m.T0), CierraEn: w.sim.HoraReal(m.Cierre),
		})
		if err != nil {
			return err
		}
		nota := "nuevo"
		if !nuevo {
			nota = "ya existia"
		}
		log.Printf("HU2-08: %s estado=%s (%s) %q", describir(p.Accion), estado, nota, m.Pregunta)
	case AccionCerrar:
		if err := w.Pagos.Cerrar(ctx, m.ID); err != nil {
			return err
		}
		log.Printf("HU2-08: %s", describir(p.Accion))
	case AccionResolver:
		if err := w.Pagos.Resolver(ctx, m.ID, p.Ganadora); err != nil {
			return err
		}
		g := p.Ganadora
		ganadora = &g
		log.Printf("HU2-08: %s gana %q", describir(p.Accion), g)
	}
	if p.vivo && estado != EstadoProgramado {
		w.difundir(m, estado, ganadora)
	}
	return nil
}

// difundir no falla la accion: Pagos ya quedo al dia.
func (w *Worker) difundir(m Mercado, estado string, ganadora *string) {
	if w.Difusor == nil {
		return
	}
	msg := MensajeDinamico{
		MarketID: m.ID, EventID: m.EventoID, Template: m.Plantilla, Question: m.Pregunta,
		Status: estado, RaceCloseS: segundoCierre(m.Cierre), ClosesAt: w.sim.HoraReal(m.Cierre),
		WinningOption: ganadora,
	}
	for _, o := range m.Opciones {
		msg.Options = append(msg.Options, opcionPagos{Opcion: o.Nombre, Multiplicador: o.Multiplicador})
	}
	if err := w.Difusor.Difundir(TipoMercadoDinamico, msg); err != nil {
		log.Printf("HU2-08: no se pudo difundir %s: %v", m.ID, err)
	}
}

func describir(a Accion) string {
	return fmt.Sprintf("%s %s market_id=%s t=%s", a.Tipo, a.Mercado.Plantilla, a.Mercado.ID, seg(a.T))
}

func logSinResolver(sim Simulacion, ms []Mercado) {
	for _, m := range ms {
		log.Printf("HU2-08: simulacion %s %s en t=%s: %s market_id=%s queda cerrado sin resolver (la observacion terminaba en t=%s)",
			sim.ID, sim.Estado, seg(sim.TCarreraMs), m.Plantilla, m.ID, seg(m.Resolucion))
	}
}

// seg muestra un t de carrera en segundos.
func seg(ms int64) string {
	if ms == math.MinInt64 {
		return "-"
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
