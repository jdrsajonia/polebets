package posiciones

import (
	"log"
	"time"

	"github.com/arquisoft/motor/internal/ws"
)

// Notificador es lo que el Difusor usa del hub de WebSocket. *ws.Hub lo
// cumple; en las pruebas se reemplaza por un hub falso.
type Notificador interface {
	Difundir(ws.Mensaje) error
}

// PilotoCarrera es la estructura mínima que el Motor necesita para armar el tablero.
// (Antes venía de 'ingest', pero ahora el Ingestor es un servicio aparte).
type PilotoCarrera struct {
	Numero int
	Nombre string
	Codigo string
	Equipo string
	Color  string
}

// Difusor conecta la reproduccion con el Tablero y el WebSocket:
// cada aviso actualiza el tablero y difunde la foto completa a todos.
type Difusor struct {
	tablero *Tablero
	hub     Notificador
	ahora   func() time.Time
}

func NuevoDifusor(tablero *Tablero, hub Notificador) *Difusor {
	return &Difusor{tablero: tablero, hub: hub, ahora: time.Now}
}

// Iniciar arranca la carrera y difunde de una vez la foto inicial.
func (d *Difusor) Iniciar(carreraID string, pilotos []PilotoCarrera, posiciones map[int]int) {
	d.difundir(d.tablero.Iniciar(carreraID, pilotos, posiciones))
}

// Actualizar aplica el cambio y difunde la foto nueva.
func (d *Difusor) Actualizar(numero, posicion int) {
	if foto, ok := d.tablero.Actualizar(numero, posicion); ok {
		d.difundir(foto)
	}
}

// Aplicar actualiza varias posiciones y la vuelta, y difunde una sola foto (HU2-14).
func (d *Difusor) Aplicar(cambios map[int]int, vuelta, total int) {
	if foto, ok := d.tablero.ActualizarVarias(cambios, vuelta, total); ok {
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
