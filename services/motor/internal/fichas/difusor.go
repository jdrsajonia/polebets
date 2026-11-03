package fichas

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/arquisoft/motor/internal/ws"
)

// Notificador es lo que el Difusor usa del hub de WebSocket. *ws.Hub lo
// cumple; en las pruebas se reemplaza por un hub falso.
type Notificador interface {
	Difundir(ws.Mensaje) error
}

// ParametrosTorneo son los datos del torneo que el ranking necesita y que no
// viven en Redis: fichas iniciales y el minimo a apostar (configuracion del
// motor). El botin es por evento y se pide a Pagos (Difusor.Pagos).
type ParametrosTorneo struct {
	FichasIniciales int64
	MinWagerPct     float64
}

// Difusor arma el ranking del torneo y lo difunde por WebSocket (RF-18). Sigue
// el patron de posiciones.Difusor (HU-05): cada cambio recalcula la foto
// completa y la envia a todos, marcando emitido_en para medir la latencia
// (RNF-13, cambio en pantalla en < 1 s).
//
// El botin (events.pot) se pide a Pagos la primera vez por evento y se guarda
// en memoria: despues de la largada no cambia. Si Pagos falla se difunde con
// 0 y se vuelve a pedir en el siguiente envio; nunca bloquea el ranking.
type Difusor struct {
	almacen *Almacen
	hub     Notificador
	params  ParametrosTorneo
	ahora   func() time.Time

	// Pagos da el botin de cada evento. nil = botin 0.
	Pagos PagosBotin

	mu      sync.Mutex
	botines map[string]int64 // evento_id -> pot ya leido de Pagos
}

func NuevoDifusor(almacen *Almacen, hub Notificador, params ParametrosTorneo) *Difusor {
	return &Difusor{almacen: almacen, hub: hub, params: params, ahora: time.Now}
}

// Difundir lee el estado actual de todos los inscritos, arma el ranking y lo
// envia. Se llama despues de cada cambio de fichas (una liquidacion de
// torneo). Si falla solo se registra: el cambio ya quedo en Redis.
func (d *Difusor) Difundir(ctx context.Context, eventoID string) {
	estados, err := d.almacen.Estados(ctx, eventoID)
	if err != nil {
		log.Printf("HU2-11: no se pudo leer el estado del torneo %s: %v", eventoID, err)
		return
	}
	ranking := ConstruirRanking(
		eventoID, estados,
		d.params.FichasIniciales, d.params.MinWagerPct, d.botin(ctx, eventoID),
		d.ahora(),
	)
	ranking.EmitidoEn = d.ahora().UTC()
	if err := d.hub.Difundir(ws.Mensaje{Type: ws.TipoRankingTorneo, Payload: ranking}); err != nil {
		log.Printf("HU2-11: no se pudo difundir el ranking del torneo %s: %v", eventoID, err)
	}
}

// botin devuelve el pot guardado o lo pide a Pagos. El candado no se tiene
// durante la llamada: dos envios a la vez pueden pedirlo los dos, sin dano.
func (d *Difusor) botin(ctx context.Context, eventoID string) int64 {
	d.mu.Lock()
	b, ok := d.botines[eventoID]
	d.mu.Unlock()
	if ok || d.Pagos == nil {
		return b
	}
	b, err := d.Pagos.Botin(ctx, eventoID)
	if err != nil {
		log.Printf("HU2-11: no se pudo leer el botin del torneo %s (se reintenta en el siguiente envio): %v", eventoID, err)
		return 0
	}
	d.mu.Lock()
	if d.botines == nil {
		d.botines = map[string]int64{}
	}
	d.botines[eventoID] = b
	d.mu.Unlock()
	return b
}

// olvidarBotines borra los de eventos que ya no estan activos.
func (d *Difusor) olvidarBotines(activos []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for e := range d.botines {
		if !slices.Contains(activos, e) {
			delete(d.botines, e)
		}
	}
}

// ReenvioRanking: cada cuanto se vuelve a difundir el ranking de los torneos
// activos aunque no cambie, para que quien entra o recarga /carrera lo vea sin
// esperar a la siguiente liquidacion (mismo patron que posiciones y el
// mercado dinamico abierto).
const ReenvioRanking = 5 * time.Second

// Reenviar difunde el ranking de cada torneo activo: los que tienen fichas en
// Redis (largaron y aun no los cierra HU2-12). Uno cancelado nunca aparece.
func (d *Difusor) Reenviar(ctx context.Context) {
	eventos, err := d.almacen.EventosActivos(ctx)
	if err != nil {
		log.Printf("HU2-11: no se pudieron listar los torneos activos: %v", err)
		return
	}
	d.olvidarBotines(eventos)
	for _, e := range eventos {
		d.Difundir(ctx, e)
	}
}

// Run llama a Reenviar cada `cada` hasta que se cancele el contexto.
func (d *Difusor) Run(ctx context.Context, cada time.Duration) {
	ticker := time.NewTicker(cada)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Reenviar(ctx)
		}
	}
}
