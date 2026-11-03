// Package eventos arma el calendario del jugador (HU2-05, RF-06, seccion 3.4
// de documentation/orquestador-api.md):
//
//	GET /eventos              (el Gateway la expone como /api/eventos)
//	GET /eventos/{evento_id}  (el Gateway la expone como /api/eventos/{id})
//
// Solo jugadores. Son lecturas: el cliente interno ya reintenta; si Pagos no
// responde tras los reintentos -> 503. El detalle hace tres lecturas a Pagos
// en paralelo (evento, mercados estaticos y la inscripcion del jugador).
package eventos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

// Servicio es lo que este flujo necesita de un cliente interno (interno.Cliente lo cumple).
type Servicio interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// ---------------------------------------------------------------------------
// API publica (nombres en espanol del contrato, seccion 3.4)
// ---------------------------------------------------------------------------

// Evento es una fila del calendario.
type Evento struct {
	EventoID         string `json:"evento_id"`
	Carrera          string `json:"carrera"`
	Estado           string `json:"estado"`
	EstadoTorneo     string `json:"estado_torneo"`
	Largada          string `json:"largada"`
	Inscritos        int64  `json:"inscritos"`
	Botin            int64  `json:"botin"`
	CostoInscripcion int64  `json:"costo_inscripcion"`
	FichasIniciales  int64  `json:"fichas_iniciales"`
}

// Opcion es una opcion de un mercado, con su multiplicador.
type Opcion struct {
	Opcion        string  `json:"opcion"`
	Multiplicador float64 `json:"multiplicador"`
}

// MercadoEstatico es un mercado estatico con sus opciones.
type MercadoEstatico struct {
	MercadoID string   `json:"mercado_id"`
	Plantilla string   `json:"plantilla"`
	Pregunta  string   `json:"pregunta"`
	Estado    string   `json:"estado"`
	CierraEn  *string  `json:"cierra_en"`
	Opciones  []Opcion `json:"opciones"`
}

// MiInscripcion dice si el jugador esta inscrito y si se le devolvio el buy_in.
type MiInscripcion struct {
	Inscrito bool `json:"inscrito"`
	Devuelta bool `json:"devuelta"`
}

// ListaEventos es la respuesta de GET /eventos.
type ListaEventos struct {
	Eventos []Evento `json:"eventos"`
}

// DetalleEvento es la respuesta de GET /eventos/{id}.
type DetalleEvento struct {
	Evento            Evento            `json:"evento"`
	MercadosEstaticos []MercadoEstatico `json:"mercados_estaticos"`
	MiInscripcion     MiInscripcion     `json:"mi_inscripcion"`
}

// ---------------------------------------------------------------------------
// Respuestas internas de Pagos
// ---------------------------------------------------------------------------

type eventoPagos struct {
	EventID          string `json:"event_id"`
	RaceKey          string `json:"race_key"`
	Status           string `json:"status"`
	TournamentStatus string `json:"tournament_status"`
	StartsAt         string `json:"starts_at"`
	Registered       int64  `json:"registered"`
	Pot              int64  `json:"pot"`
	BuyIn            int64  `json:"buy_in"`
	InitialChips     int64  `json:"initial_chips"`
}

func (e eventoPagos) aPublico() Evento {
	return Evento{
		EventoID:         e.EventID,
		Carrera:          e.RaceKey,
		Estado:           e.Status,
		EstadoTorneo:     e.TournamentStatus,
		Largada:          e.StartsAt,
		Inscritos:        e.Registered,
		Botin:            e.Pot,
		CostoInscripcion: e.BuyIn,
		FichasIniciales:  e.InitialChips,
	}
}

type mercadoPagos struct {
	MarketID string  `json:"market_id"`
	Template string  `json:"template"`
	Question string  `json:"question"`
	Status   string  `json:"status"`
	ClosesAt *string `json:"closes_at"`
	Options  []struct {
		Opcion        string  `json:"opcion"`
		Multiplicador float64 `json:"multiplicador"`
	} `json:"options"`
}

func (m mercadoPagos) aPublico() MercadoEstatico {
	opciones := make([]Opcion, 0, len(m.Options))
	for _, o := range m.Options {
		opciones = append(opciones, Opcion{Opcion: o.Opcion, Multiplicador: o.Multiplicador})
	}
	return MercadoEstatico{
		MercadoID: m.MarketID,
		Plantilla: m.Template,
		Pregunta:  m.Question,
		Estado:    m.Status,
		CierraEn:  m.ClosesAt,
		Opciones:  opciones,
	}
}

type inscripcionPagos struct {
	TournamentStatus string `json:"tournament_status"`
	Refunded         bool   `json:"refunded"`
}

// ---------------------------------------------------------------------------
// Rutas
// ---------------------------------------------------------------------------

// Register agrega las dos rutas del calendario, protegidas por sesion.
func Register(mux *http.ServeMux, pagos Servicio) {
	mux.Handle("GET /eventos", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			lista, err := Listar(r.Context(), pagos)
			if err != nil {
				errores.Responder(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store") // inscritos y botin cambian en vivo
			httpx.EscribirJSON(w, http.StatusOK, lista)
		},
	)))

	mux.Handle("GET /eventos/{evento_id}", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			userID, _ := sesion.UsuarioDe(r.Context())
			detalle, err := Detalle(r.Context(), pagos, r.PathValue("evento_id"), userID)
			if err != nil {
				errores.Responder(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.EscribirJSON(w, http.StatusOK, detalle)
		},
	)))
}

// Listar pide a Pagos los eventos programados o en curso.
func Listar(ctx context.Context, pagos Servicio) (ListaEventos, error) {
	resp, err := pagos.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodGet,
		Ruta:   "/internal/events?status=programado,en_curso",
	})
	if err != nil {
		return ListaEventos{}, err // ErrNoDisponible -> 503
	}
	if resp.Estado != http.StatusOK {
		return ListaEventos{}, fmt.Errorf("pagos respondio %d al listar eventos: %s", resp.Estado, resp.Cuerpo)
	}
	var crudos []eventoPagos
	if err := json.Unmarshal(resp.Cuerpo, &crudos); err != nil {
		return ListaEventos{}, fmt.Errorf("respuesta invalida de Pagos al listar eventos: %w", err)
	}
	eventos := make([]Evento, 0, len(crudos))
	for _, e := range crudos {
		eventos = append(eventos, e.aPublico())
	}
	return ListaEventos{Eventos: eventos}, nil
}

// Detalle junta el evento, sus mercados estaticos y la inscripcion del jugador
// en una sola respuesta, con las tres lecturas a Pagos en paralelo.
func Detalle(ctx context.Context, pagos Servicio, eventoID, userID string) (DetalleEvento, error) {
	eventoID = strings.ToLower(strings.TrimSpace(eventoID))
	if !esUUID(eventoID) {
		// Un id con otra forma no puede existir; igual que uno inexistente.
		return DetalleEvento{}, errores.EventoNoExiste
	}

	var (
		wg         sync.WaitGroup
		evento     eventoPagos
		errEvento  error
		mercados   []mercadoPagos
		errMercado error
		inscrito   bool
		devuelta   bool
	)
	id := url.PathEscape(eventoID)

	wg.Add(3)
	go func() {
		defer wg.Done()
		errEvento = leerEvento(ctx, pagos, id, &evento)
	}()
	go func() {
		defer wg.Done()
		errMercado = leerJSON(ctx, pagos, "/internal/events/"+id+"/markets?kind=estatica", &mercados)
	}()
	go func() {
		defer wg.Done()
		inscrito, devuelta = leerInscripcion(ctx, pagos, id, url.PathEscape(userID))
	}()
	wg.Wait()

	if errEvento != nil {
		return DetalleEvento{}, errEvento
	}
	if errMercado != nil {
		return DetalleEvento{}, errMercado
	}

	estaticos := make([]MercadoEstatico, 0, len(mercados))
	for _, m := range mercados {
		estaticos = append(estaticos, m.aPublico())
	}
	return DetalleEvento{
		Evento:            evento.aPublico(),
		MercadosEstaticos: estaticos,
		MiInscripcion:     MiInscripcion{Inscrito: inscrito, Devuelta: devuelta},
	}, nil
}

// leerEvento traduce el 404 de Pagos a evento_no_existe.
func leerEvento(ctx context.Context, pagos Servicio, id string, destino *eventoPagos) error {
	resp, err := pagos.Hacer(ctx, interno.Peticion{Metodo: http.MethodGet, Ruta: "/internal/events/" + id})
	if err != nil {
		return err // ErrNoDisponible -> 503
	}
	switch resp.Estado {
	case http.StatusOK:
		if err := json.Unmarshal(resp.Cuerpo, destino); err != nil {
			return fmt.Errorf("respuesta invalida de Pagos al leer el evento: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return errores.EventoNoExiste
	default:
		return fmt.Errorf("pagos respondio %d al leer el evento: %s", resp.Estado, resp.Cuerpo)
	}
}

// leerJSON hace un GET y decodifica el 200; cualquier otro estado es error.
func leerJSON(ctx context.Context, pagos Servicio, ruta string, destino any) error {
	resp, err := pagos.Hacer(ctx, interno.Peticion{Metodo: http.MethodGet, Ruta: ruta})
	if err != nil {
		return err
	}
	if resp.Estado != http.StatusOK {
		return fmt.Errorf("pagos respondio %d en %s: %s", resp.Estado, ruta, resp.Cuerpo)
	}
	if err := json.Unmarshal(resp.Cuerpo, destino); err != nil {
		return fmt.Errorf("respuesta invalida de Pagos en %s: %w", ruta, err)
	}
	return nil
}

// leerInscripcion consulta si el jugador esta inscrito. 404 = no inscrito (no
// es error). Si Pagos falla, se asume no inscrito: el calendario no se rompe
// por eso (el dato es secundario frente al evento y sus mercados).
func leerInscripcion(ctx context.Context, pagos Servicio, eventoID, userID string) (inscrito, devuelta bool) {
	resp, err := pagos.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodGet,
		Ruta:   "/internal/events/" + eventoID + "/registrations/" + userID,
	})
	if err != nil || resp.Estado != http.StatusOK {
		return false, false
	}
	var i inscripcionPagos
	if err := json.Unmarshal(resp.Cuerpo, &i); err != nil {
		return false, false
	}
	return true, i.Refunded
}

// esUUID revisa el formato 8-4-4-4-12 en hexadecimal (minusculas).
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
