package posiciones

import (
	"log"
	"time"

	"github.com/arquisoft/motor/internal/ingest"
	"github.com/arquisoft/motor/internal/ws"
)

// Notificador es lo que el Difusor usa del hub de WebSocket. *ws.Hub lo
// cumple; en las pruebas se reemplaza por un hub falso.
type Notificador interface {
	Difundir(ws.Mensaje) error
}

// Difusor conecta la reproduccion (ingest) con el Tablero y el WebSocket:
// cada aviso actualiza el tablero y difunde la foto completa a todos.
type Difusor struct {
	tablero *Tablero
	hub     Notificador
	ahora   func() time.Time
}

// Si Difusor deja de cumplir el contrato de ingest, esto no compila.
var _ ingest.ObservadorCarrera = (*Difusor)(nil)

func NuevoDifusor(tablero *Tablero, hub Notificador) *Difusor {
	return &Difusor{tablero: tablero, hub: hub, ahora: time.Now}
}

// Iniciar arranca la carrera y difunde de una vez la foto inicial.
func (d *Difusor) Iniciar(carreraID string, pilotos []ingest.PilotoCarrera, posiciones map[int]int) {
	d.difundir(d.tablero.Iniciar(carreraID, pilotos, posiciones))
}

// Actualizar aplica el cambio y difunde la foto nueva.
func (d *Difusor) Actualizar(numero, posicion int) {
	if foto, ok := d.tablero.Actualizar(numero, posicion); ok {
		d.difundir(foto)
	}
}

// difundir marca emitido_en (para medir la latencia, RNF-04) y envia. Si
// falla solo se registra: la reproduccion sigue.
func (d *Difusor) difundir(foto FotoPosiciones) {
	foto.EmitidoEn = d.ahora().UTC()
	if err := d.hub.Difundir(ws.Mensaje{Type: ws.TipoPosiciones, Payload: foto}); err != nil {
		log.Printf("HU-05: no se pudo difundir posiciones: %v", err)
	}
}
