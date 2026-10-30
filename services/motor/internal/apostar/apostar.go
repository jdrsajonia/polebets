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
//   - Modo torneo (dinamicas de un inscrito con torneo activo): fichas en
//     Redis. PENDIENTE: se activa cuando exista el almacen de fichas
//     (HU2-11); mientras Fichas sea nil todas las apuestas son individuales.
//
// Una apuesta por mercado (bets.uq_bets_user_market). El id de la apuesta se
// deriva de (usuario, Idempotency-Key): un reintento con la misma clave da la
// misma apuesta y no debita dos veces.
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
	ErrPagosNoDisponible = errors.New("pagos no esta disponible")
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
}

// Fichas decide si una apuesta va en modo torneo (HU2-11). nil = no hay
// torneos activos todavia: todo es individual.
type Fichas interface {
	TieneFichas(ctx context.Context, eventoID, userID string) (bool, error)
}

// Servicio reune la logica de HU2-09.
type Servicio struct {
	Pagos         Pagos
	Fichas        Fichas
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

	modo, err := s.modo(ctx, m, p.UserID)
	if err != nil {
		return Apuesta{}, err
	}
	if modo == ModoTorneo {
		// No se llega aqui mientras Fichas sea nil (ver comentario del paquete).
		return Apuesta{}, errors.New("modo torneo aun no implementado (HU2-11)")
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

// modo: las estaticas siempre son individuales; las dinamicas son torneo si
// el jugador tiene fichas del evento (inscrito con torneo activo).
func (s *Servicio) modo(ctx context.Context, m Mercado, userID string) (string, error) {
	if m.Tipo == TipoEstatica || s.Fichas == nil {
		return ModoIndividual, nil
	}
	tiene, err := s.Fichas.TieneFichas(ctx, m.EventoID, userID)
	if err != nil {
		return "", err
	}
	if tiene {
		return ModoTorneo, nil
	}
	return ModoIndividual, nil
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
