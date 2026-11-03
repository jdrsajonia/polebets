package estaticas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// KindEstatica ya esta en catalogo.go. Estados del mercado en Pagos:
const (
	// EstadoAbierto: el mercado acepta apuestas (antes de la largada).
	EstadoAbierto = "abierto"
	// EstadoProgramado: creado pero sin aceptar apuestas; se usa si el Motor
	// arranca con la carrera ya largada (nadie apuesta tarde).
	EstadoProgramado = "programado"
)

// Errores del cliente de Pagos. El worker decide con errors.Is:
var (
	// ErrReintentable: red, 5xx o 429. Se reintenta en el siguiente tick.
	ErrReintentable = errors.New("pagos: error reintentable")
	// ErrConflicto: 409. Pagos tiene otros datos para ese id; reintentar no lo
	// arregla. Se loguea fuerte.
	ErrConflicto = errors.New("pagos: conflicto")
	// ErrRechazado: otro 4xx (token, validacion, recurso inexistente). Tampoco
	// se arregla reintentando.
	ErrRechazado = errors.New("pagos: peticion rechazada")
)

// AltaEvento es lo que el worker manda al crear el evento (POST
// /internal/events). buy_in, initial_chips y commission los decide Pagos con
// su configuracion; el Motor solo fija el evento y su largada.
type AltaEvento struct {
	EventoID    string
	RaceKey     string
	HoraLargada time.Time
}

// AltaMercado es lo que el worker manda al crear un mercado estatico.
type AltaMercado struct {
	MercadoID string
	EventoID  string
	Plantilla string
	Pregunta  string
	Opciones  []Opcion
	Estado    string // "abierto" o, en una recuperacion, "programado"
}

// Largada es lo que Pagos devuelve al cerrar las inscripciones
// (POST /internal/events/{id}/start): a quienes darles fichas de torneo.
type Largada struct {
	Jugadores       []string
	FichasIniciales int64
}

// Pagos es lo que el worker necesita de Pagos.
type Pagos interface {
	// CrearEvento devuelve nuevo=true con 201 y false con 200 (ya existia).
	CrearEvento(ctx context.Context, a AltaEvento) (nuevo bool, err error)
	// CrearMercado devuelve nuevo=true con 201 y false con 200.
	CrearMercado(ctx context.Context, a AltaMercado) (nuevo bool, err error)
	Cerrar(ctx context.Context, mercadoID string) error
	Resolver(ctx context.Context, mercadoID, opcion string) error
	// Largar cierra las inscripciones y devuelve los jugadores del torneo.
	Largar(ctx context.Context, eventoID string) (Largada, error)
	// Finalizar cierra la carrera de un evento con torneo cancelado (sin
	// reparto): POST /internal/events/{id}/close con players vacios. Solo se
	// usa cuando la largada no dejo torneo (HU2-12 cierra los torneos activos).
	Finalizar(ctx context.Context, eventoID string) error
}

// PagosHTTP implementa Pagos con las rutas /internal/* y el token de servicio.
type PagosHTTP struct {
	BaseURL string
	Token   string
	Cliente *http.Client
}

func NuevoPagosHTTP(baseURL, token string) *PagosHTTP {
	return &PagosHTTP{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Cliente: &http.Client{Timeout: 5 * time.Second},
	}
}

type opcionPagos struct {
	Opcion        string  `json:"opcion"`
	Multiplicador float64 `json:"multiplicador"`
}

type cuerpoEvento struct {
	EventID  string    `json:"event_id"`
	RaceKey  string    `json:"race_key"`
	StartsAt time.Time `json:"starts_at"` // UTC: sale con "Z" (Pagos exige zona)
}

func (p *PagosHTTP) CrearEvento(ctx context.Context, a AltaEvento) (bool, error) {
	cuerpo := cuerpoEvento{EventID: a.EventoID, RaceKey: a.RaceKey, StartsAt: a.HoraLargada.UTC()}
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/events", cuerpo)
	if err != nil {
		return false, err
	}
	if err := clasificar("crear evento "+a.EventoID, estado, b); err != nil {
		return false, err
	}
	return estado == http.StatusCreated, nil
}

type cuerpoMercado struct {
	MarketID string        `json:"market_id"`
	EventID  string        `json:"event_id"`
	Kind     string        `json:"kind"`
	Template string        `json:"template"`
	Question string        `json:"question"`
	Options  []opcionPagos `json:"options"`
	Status   string        `json:"status"`
}

func (p *PagosHTTP) CrearMercado(ctx context.Context, a AltaMercado) (bool, error) {
	cuerpo := cuerpoMercado{
		MarketID: a.MercadoID, EventID: a.EventoID, Kind: KindEstatica,
		Template: a.Plantilla, Question: a.Pregunta, Status: a.Estado,
	}
	for _, o := range a.Opciones {
		cuerpo.Options = append(cuerpo.Options, opcionPagos{Opcion: o.Nombre, Multiplicador: o.Multiplicador})
	}
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/markets", cuerpo)
	if err != nil {
		return false, err
	}
	if estado == http.StatusNotFound {
		return false, fmt.Errorf("%w: el evento %s aun no existe en Pagos", ErrReintentable, a.EventoID)
	}
	if err := clasificar("crear mercado "+a.MercadoID, estado, b); err != nil {
		return false, err
	}
	return estado == http.StatusCreated, nil
}

func (p *PagosHTTP) Cerrar(ctx context.Context, mercadoID string) error {
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/markets/"+url.PathEscape(mercadoID)+"/close", nil)
	if err != nil {
		return err
	}
	return clasificar("cerrar "+mercadoID, estado, b)
}

func (p *PagosHTTP) Resolver(ctx context.Context, mercadoID, opcion string) error {
	cuerpo := map[string]string{"winning_option": opcion}
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/markets/"+url.PathEscape(mercadoID)+"/resolve", cuerpo)
	if err != nil {
		return err
	}
	return clasificar("resolver "+mercadoID, estado, b)
}

type respuestaLargada struct {
	Players      []string `json:"players"`
	InitialChips int64    `json:"initial_chips"`
}

func (p *PagosHTTP) Largar(ctx context.Context, eventoID string) (Largada, error) {
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/events/"+url.PathEscape(eventoID)+"/start", nil)
	if err != nil {
		return Largada{}, err
	}
	if err := clasificar("largar "+eventoID, estado, b); err != nil {
		return Largada{}, err
	}
	var r respuestaLargada
	if err := json.Unmarshal(b, &r); err != nil {
		return Largada{}, fmt.Errorf("%w: respuesta de start invalida: %v", ErrReintentable, err)
	}
	return Largada{Jugadores: r.Players, FichasIniciales: r.InitialChips}, nil
}

// Finalizar cierra la carrera de un evento con torneo cancelado: POST
// /internal/events/{id}/close con players vacios. Pagos reconoce el torneo
// cancelado y solo marca el evento finalizado, sin repartir (HU2-12). Exige
// Idempotency-Key = event-close:{id} (la misma clave que usa el cierre con
// reparto): asi repetirlo es inofensivo y nunca paga.
func (p *PagosHTTP) Finalizar(ctx context.Context, eventoID string) error {
	cuerpo, err := json.Marshal(map[string]any{"players": []string{}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.BaseURL+"/internal/events/"+url.PathEscape(eventoID)+"/close", bytes.NewReader(cuerpo))
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Service-Token", p.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "event-close:"+eventoID)

	resp, err := p.Cliente.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrReintentable, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return clasificar("finalizar "+eventoID, resp.StatusCode, b)
}

// clasificar: 200 y 201 son exito.
func clasificar(op string, estado int, cuerpo []byte) error {
	switch {
	case estado == http.StatusOK || estado == http.StatusCreated:
		return nil
	case estado == http.StatusConflict:
		return fmt.Errorf("%w: %s respondio 409: %s", ErrConflicto, op, recortar(cuerpo))
	case estado >= 500 || estado == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s respondio %d", ErrReintentable, op, estado)
	default:
		return fmt.Errorf("%w: %s respondio %d: %s", ErrRechazado, op, estado, recortar(cuerpo))
	}
}

func recortar(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

func (p *PagosHTTP) pedir(ctx context.Context, metodo, ruta string, cuerpo any) (int, []byte, error) {
	var lector io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return 0, nil, err
		}
		lector = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, p.BaseURL+ruta, lector)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Internal-Service-Token", p.Token)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.Cliente.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrReintentable, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}
