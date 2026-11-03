// Package apostar implementa HU2-09: apostar en un mercado abierto, en modo
// individual o torneo (seccion 3.6 de documentation/orquestador-api.md).
//
//	Orquestador ──POST /internal/apuestas──► Motor ──► Pagos
//
// El Motor es el dueño de la apuesta: valida el mercado abierto, la opcion,
// el modo, el tope individual y las fichas. Pagos guarda el dinero.
//
//   - Modo individual (estaticas siempre; dinamicas si el jugador no tiene
//     fichas): tope de MaxIndividual tokens; Pagos debita y registra la
//     apuesta en una sola transaccion (POST /internal/debit).
//   - Modo torneo (dinamicas de un inscrito con torneo activo): registra la
//     apuesta en Pagos sin tokens (POST /internal/bets/torneo, RF-12/RNF-20)
//     y despues descuenta fichas en Redis con el almacen de HU2-11
//     (fichas.ReservarApuesta). Sin tope de tokens: el limite son las
//     fichas. Si Fichas es nil (Motor sin Redis) todas son individuales.
//
// En los dos modos la apuesta guarda la vuelta actual (bets.lap, RF-27), que
// se lee de la simulacion del Ingestor (Carrera). Sin carrera en curso queda
// nula.
//
// Una apuesta por mercado. Individual: el id se deriva de (usuario,
// Idempotency-Key) y Pagos lo hace cumplir (bets.uq_bets_user_market).
// Torneo: el id se deriva de (usuario, mercado), asi que cualquier reintento
// cae en la misma reserva y no descuenta dos veces.
//
// Este paquete reemplaza la ruta del P1 (POST /apuestas con JWT, paquete
// apuestas). Ese paquete sigue vivo porque lo usa la liquidacion del P1.
package apostar

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Errores de negocio. Cada uno tiene su codigo publico (ver handler.go).
var (
	ErrPeticionInvalida  = errors.New("peticion invalida")
	ErrMercadoNoExiste   = errors.New("el mercado no existe")
	ErrMercadoCerrado    = errors.New("mercado cerrado")
	ErrOpcionInvalida    = errors.New("la opcion no pertenece al mercado")
	ErrMontoInvalido     = errors.New("monto invalido")
	ErrYaApostaste       = errors.New("ya hay una apuesta en ese mercado")
	ErrClaveReutilizada  = errors.New("clave reutilizada con otro contenido")
	ErrSaldoInsuficiente = errors.New("saldo insuficiente")
	// ErrFichasInsuficientes: modo torneo y el jugador no tiene fichas
	// suficientes (quien llega a 0 no puede apostar).
	ErrFichasInsuficientes = errors.New("fichas insuficientes")
	ErrPagosNoDisponible   = errors.New("pagos no esta disponible")
)

// Valores de markets.status y markets.kind (modelo de datos del P2).
const (
	EstadoAbierto  = "abierto"
	TipoEstatica   = "estatica"
	ModoIndividual = "individual"
	ModoTorneo     = "torneo"
)

// Opcion de un mercado con su multiplicador tal como lo guarda Pagos.
type Opcion struct {
	Opcion        string      `json:"opcion"`
	Multiplicador json.Number `json:"multiplicador"`
}

// Mercado es lo que el Motor lee de Pagos (GET /internal/markets/{id}).
type Mercado struct {
	ID       string     `json:"market_id"`
	EventoID string     `json:"event_id"`
	Tipo     string     `json:"kind"`
	Estado   string     `json:"status"`
	Opciones []Opcion   `json:"options"`
	CierraEn *time.Time `json:"closes_at"`
}

// Peticion es lo que llega del Orquestador.
type Peticion struct {
	UserID    string `json:"user_id"`
	MercadoID string `json:"mercado_id"`
	Opcion    string `json:"opcion"`
	Monto     int64  `json:"monto"`
}

// Debito es el cuerpo de POST /internal/debit de Pagos (T2-03).
type Debito struct {
	BetID          string      `json:"bet_id"`
	UserID         string      `json:"user_id"`
	MarketID       string      `json:"market_id"`
	Option         string      `json:"option"`
	Amount         int64       `json:"amount"`
	Multiplier     json.Number `json:"multiplier"`
	Lap            *int        `json:"lap"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// ApuestaTorneo es el cuerpo de POST /internal/bets/torneo de Pagos. No mueve
// tokens: las fichas viven en Redis.
type ApuestaTorneo struct {
	BetID      string      `json:"bet_id"`
	UserID     string      `json:"user_id"`
	MarketID   string      `json:"market_id"`
	Option     string      `json:"option"`
	Amount     int64       `json:"amount"`
	Multiplier json.Number `json:"multiplier"`
	Lap        *int        `json:"lap"`
}

// ResultadoDebito es lo que importa de la respuesta de Pagos.
type ResultadoDebito struct {
	Saldo int64
	Nueva bool
}

// Apuesta es la respuesta del Motor al Orquestador.
type Apuesta struct {
	ApuestaID     string      `json:"apuesta_id"`
	MercadoID     string      `json:"mercado_id"`
	Opcion        string      `json:"opcion"`
	Monto         int64       `json:"monto"`
	Modo          string      `json:"modo"`
	Multiplicador json.Number `json:"multiplicador"`
	PagoSiAcierta int64       `json:"pago_si_acierta"`
	Saldo         *int64      `json:"saldo"`  // tokens que quedan (individual)
	Fichas        *int64      `json:"fichas"` // fichas que quedan (torneo)
	Nueva         bool        `json:"nueva"`
}

// Pagos es lo que este flujo necesita del servicio de Pagos.
type Pagos interface {
	Mercado(ctx context.Context, id string) (Mercado, error)
	Debitar(ctx context.Context, d Debito) (ResultadoDebito, error)
	// RegistrarTorneo guarda la apuesta de torneo (idempotente por bet_id).
	// Devuelve nueva=false si ya estaba. Errores: ErrMercadoCerrado,
	// ErrYaApostaste, ErrMercadoNoExiste, ErrPagosNoDisponible.
	RegistrarTorneo(ctx context.Context, a ApuestaTorneo) (bool, error)
}

// Carrera da la vuelta actual de la carrera en curso (bets.lap, RF-27). El
// adaptador real lee simulaciones.vuelta_actual con el lector de HU2-14
// (main.go). ok=false si no hay carrera en curso. nil = sin lectura de vuelta.
type Carrera interface {
	VueltaActual(ctx context.Context) (vuelta int, ok bool, err error)
}

// EstadoFichas es el saldo de un jugador en el torneo de un evento.
type EstadoFichas struct {
	Fichas   int64 // fichas que le quedan
	Apostado int64 // total apostado en el torneo
}

// ReservaFichas es una apuesta de torneo (se descuenta de las fichas).
type ReservaFichas struct {
	ApuestaID     string
	UserID        string
	Opcion        string
	Monto         int64
	Multiplicador float64
}

// Fichas es lo que este flujo necesita del almacen del torneo (HU2-11). El
// adaptador real vive en main.go. nil = Motor sin Redis: todo es individual.
type Fichas interface {
	// Jugador devuelve el estado del usuario si esta inscrito en el torneo
	// activo del evento; ok=false si no hay torneo o no esta inscrito.
	Jugador(ctx context.Context, eventoID, userID string) (EstadoFichas, bool, error)
	// Reservar descuenta las fichas. Es idempotente por ApuestaID. Errores:
	// ErrFichasInsuficientes, ErrYaApostaste (misma apuesta con otro
	// contenido) o cualquier otro (servicio no disponible).
	Reservar(ctx context.Context, eventoID string, r ReservaFichas) (EstadoFichas, error)
}

// Servicio reune la logica de HU2-09.
type Servicio struct {
	Pagos         Pagos
	Fichas        Fichas
	Carrera       Carrera
	MaxIndividual int64            // tope por apuesta individual en tokens [500]
	Ahora         func() time.Time // reloj del servidor; reemplazable en pruebas
}

func NuevoServicio(p Pagos, maxIndividual int64) *Servicio {
	return &Servicio{Pagos: p, MaxIndividual: maxIndividual, Ahora: time.Now}
}

// Apostar valida y registra la apuesta. clave es la Idempotency-Key del front.
func (s *Servicio) Apostar(ctx context.Context, p Peticion, clave string) (Apuesta, error) {
	p.UserID = strings.ToLower(strings.TrimSpace(p.UserID))
	p.MercadoID = strings.ToLower(strings.TrimSpace(p.MercadoID))
	p.Opcion = strings.TrimSpace(p.Opcion)
	clave = strings.TrimSpace(clave)
	if !esUUID(p.UserID) || p.Opcion == "" || clave == "" {
		return Apuesta{}, ErrPeticionInvalida
	}
	if !esUUID(p.MercadoID) {
		return Apuesta{}, ErrMercadoNoExiste
	}
	if p.Monto <= 0 {
		return Apuesta{}, ErrMontoInvalido
	}

	m, err := s.Pagos.Mercado(ctx, p.MercadoID)
	if err != nil {
		return Apuesta{}, err
	}
	// Reloj del servidor (RNF-19). PENDIENTE: con el reloj de carrera del
	// Ingestor, las dinamicas cerraran por race_close_s.
	if m.Estado != EstadoAbierto || (m.CierraEn != nil && !s.Ahora().Before(*m.CierraEn)) {
		return Apuesta{}, ErrMercadoCerrado
	}
	multiplicador, ok := buscarOpcion(m.Opciones, p.Opcion)
	if !ok {
		return Apuesta{}, ErrOpcionInvalida
	}
	centesimas, err := centesimasDe(multiplicador)
	if err != nil {
		return Apuesta{}, fmt.Errorf("multiplicador invalido en el mercado %s: %w", m.ID, err)
	}

	estado, torneo, err := s.modo(ctx, m, p.UserID)
	if err != nil {
		return Apuesta{}, err
	}
	lap := s.vuelta(ctx)
	if torneo {
		return s.apostarTorneo(ctx, p, m, multiplicador, centesimas, estado, lap)
	}

	if p.Monto > s.MaxIndividual {
		return Apuesta{}, ErrMontoInvalido
	}
	betID := idDeterminista(p.UserID, clave)
	r, err := s.Pagos.Debitar(ctx, Debito{
		BetID:          betID,
		UserID:         p.UserID,
		MarketID:       m.ID,
		Option:         p.Opcion,
		Amount:         p.Monto,
		Multiplier:     multiplicador,
		Lap:            lap,
		IdempotencyKey: "bet-debit:" + betID,
	})
	if err != nil {
		return Apuesta{}, err
	}
	saldo := r.Saldo
	return Apuesta{
		ApuestaID:     betID,
		MercadoID:     m.ID,
		Opcion:        p.Opcion,
		Monto:         p.Monto,
		Modo:          ModoIndividual,
		Multiplicador: multiplicador,
		PagoSiAcierta: p.Monto * centesimas / 100, // trunca como Pagos (ROUND_DOWN)
		Saldo:         &saldo,
		Nueva:         r.Nueva,
	}, nil
}

// apostarTorneo registra la apuesta en Pagos y despues descuenta las fichas
// en Redis. Los dos pasos son idempotentes por apuestaID, que sale de
// (usuario, mercado): un reintento del front repite los dos y completa lo que
// haya quedado a medias.
//
// Si Redis falla DESPUES de Pagos, la apuesta queda en bets como pending sin
// reserva de fichas y el jugador recibe 503. Al reintentar (misma u otra
// Idempotency-Key, mismo mercado), Pagos responde 200 y la reserva se hace
// entonces. Si nunca reintenta, queda una apuesta pending sin fichas
// descontadas; el liquidador (HU2-10) la encuentra sin reserva.
func (s *Servicio) apostarTorneo(ctx context.Context, p Peticion, m Mercado, multiplicador json.Number, centesimas int64, antes EstadoFichas, lap *int) (Apuesta, error) {
	apuestaID := idDeterminista(p.UserID, "torneo|"+m.ID)
	reserva := ReservaFichas{
		ApuestaID:     apuestaID,
		UserID:        p.UserID,
		Opcion:        p.Opcion,
		Monto:         p.Monto,
		Multiplicador: float64(centesimas) / 100,
	}

	// Si el monto supera las fichas solo puede ser el reintento de una apuesta
	// ya reservada (sus fichas ya se descontaron): se salta Pagos y Reservar
	// lo confirma sin escribir. Si no existe, Reservar devuelve fichas
	// insuficientes y Pagos no se toco: no queda una apuesta sin fichas.
	if p.Monto <= antes.Fichas {
		if _, err := s.Pagos.RegistrarTorneo(ctx, ApuestaTorneo{
			BetID:      apuestaID,
			UserID:     p.UserID,
			MarketID:   m.ID,
			Option:     p.Opcion,
			Amount:     p.Monto,
			Multiplier: multiplicador,
			Lap:        lap,
		}); err != nil {
			return Apuesta{}, err
		}
	}
	despues, err := s.Fichas.Reservar(ctx, m.EventoID, reserva)
	if err != nil {
		return Apuesta{}, err
	}
	fichas := despues.Fichas
	return Apuesta{
		ApuestaID:     apuestaID,
		MercadoID:     m.ID,
		Opcion:        p.Opcion,
		Monto:         p.Monto,
		Modo:          ModoTorneo,
		Multiplicador: multiplicador,
		PagoSiAcierta: p.Monto * centesimas / 100,
		Fichas:        &fichas,
		// Si lo apostado no subio, la reserva ya existia (reintento).
		Nueva: despues.Apostado > antes.Apostado,
	}, nil
}

// vuelta devuelve la vuelta actual para bets.lap, o nil si no hay carrera en
// curso. Un fallo al leerla no impide apostar: lap es informativa (RF-27).
func (s *Servicio) vuelta(ctx context.Context) *int {
	if s.Carrera == nil {
		return nil
	}
	v, ok, err := s.Carrera.VueltaActual(ctx)
	if err != nil {
		log.Printf("HU2-09: no se pudo leer la vuelta actual, la apuesta queda sin lap: %v", err)
		return nil
	}
	if !ok {
		return nil
	}
	return &v
}

// modo: las estaticas siempre son individuales; las dinamicas son torneo si
// el jugador esta inscrito en el torneo activo del evento del mercado.
func (s *Servicio) modo(ctx context.Context, m Mercado, userID string) (EstadoFichas, bool, error) {
	if m.Tipo == TipoEstatica || s.Fichas == nil || m.EventoID == "" {
		return EstadoFichas{}, false, nil
	}
	return s.Fichas.Jugador(ctx, m.EventoID, userID)
}

func buscarOpcion(opciones []Opcion, nombre string) (json.Number, bool) {
	for _, o := range opciones {
		if o.Opcion == nombre {
			return o.Multiplicador, true
		}
	}
	return "", false
}

// centesimasDe convierte "2.5" en 250 sin pasar por float (evita errores de
// redondeo en el pago). Admite hasta 2 decimales, como bets.multiplier.
func centesimasDe(n json.Number) (int64, error) {
	s := string(n)
	entero, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("mas de 2 decimales: %s", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	v, err := strconv.ParseInt(entero+frac, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("multiplicador no valido: %s", s)
	}
	return v, nil
}

// idDeterminista deriva un UUID estable de (usuario, clave), igual que el
// paquete apuestas del P1: misma clave del mismo usuario, misma apuesta.
func idDeterminista(userID, clave string) string {
	h := sha256.Sum256([]byte(userID + "|" + clave))
	h[6] = (h[6] & 0x0f) | 0x40 // version 4
	h[8] = (h[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func esUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
				return false
			}
		}
	}
	return true
}
