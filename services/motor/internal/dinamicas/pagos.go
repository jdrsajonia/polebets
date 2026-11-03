package dinamicas

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

// Errores del cliente de Pagos. El worker decide con errors.Is:
var (
	// ErrReintentable: red, 5xx, 429 o evento que aun no existe (404 al
	// crear). Se reintenta en el siguiente tick sin avanzar.
	ErrReintentable = errors.New("pagos: error reintentable")
	// ErrConflicto: 409. Pagos tiene otros datos para ese mercado; reintentar
	// no lo arregla. Se loguea fuerte.
	ErrConflicto = errors.New("pagos: conflicto")
	// ErrRechazado: otro 4xx (token, validacion, mercado inexistente al
	// abrir/cerrar/resolver). Tampoco se arregla reintentando.
	ErrRechazado = errors.New("pagos: peticion rechazada")
)

// KindDinamica es markets.kind de los mercados de HU2-08.
const KindDinamica = "dinamica"

// Alta es lo que el worker manda al crear un mercado.
type Alta struct {
	Mercado  Mercado
	Estado   string    // "abierto" o, en una recuperacion, "programado"
	AbreEn   time.Time // opens_at (Simulacion.HoraReal(T0))
	CierraEn time.Time // closes_at (Simulacion.HoraReal(Cierre))
}

// Pagos es lo que el worker necesita de Pagos (escritura de mercados).
type Pagos interface {
	// Crear devuelve nuevo=true con 201 y false con 200 (ya existia igual).
	Crear(ctx context.Context, a Alta) (nuevo bool, err error)
	Abrir(ctx context.Context, mercadoID string) error
	Cerrar(ctx context.Context, mercadoID string) error
	Resolver(ctx context.Context, mercadoID, opcion string) error
}

// PagosHTTP implementa Pagos con las rutas /internal/markets y el token de
// servicio (contrato en services/pagos/README.md, "Escritura de mercados").
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

type cuerpoCrear struct {
	MarketID   string        `json:"market_id"`
	EventID    string        `json:"event_id"`
	Kind       string        `json:"kind"`
	Template   string        `json:"template"`
	Question   string        `json:"question"`
	Options    []opcionPagos `json:"options"`
	Status     string        `json:"status"`
	OpensAt    time.Time     `json:"opens_at"` // UTC: sale con "Z" (Pagos exige zona)
	ClosesAt   time.Time     `json:"closes_at"`
	RaceCloseS int64         `json:"race_close_s"` // segundo de carrera del cierre
}

func (p *PagosHTTP) Crear(ctx context.Context, a Alta) (bool, error) {
	m := a.Mercado
	cuerpo := cuerpoCrear{
		MarketID: m.ID, EventID: m.EventoID, Kind: KindDinamica, Template: m.Plantilla,
		Question: m.Pregunta, Status: a.Estado,
		OpensAt: a.AbreEn.UTC(), ClosesAt: a.CierraEn.UTC(), RaceCloseS: segundoCierre(m.Cierre),
	}
	for _, o := range m.Opciones {
		cuerpo.Options = append(cuerpo.Options, opcionPagos{Opcion: o.Nombre, Multiplicador: o.Multiplicador})
	}
	estado, b, err := p.pedir(ctx, "/internal/markets", cuerpo)
	if err != nil {
		return false, err
	}
	if estado == http.StatusNotFound {
		return false, fmt.Errorf("%w: el evento %s aun no existe en Pagos", ErrReintentable, m.EventoID)
	}
	if err := clasificar("crear "+m.ID, estado, b); err != nil {
		return false, err
	}
	return estado == http.StatusCreated, nil
}

func (p *PagosHTTP) Abrir(ctx context.Context, mercadoID string) error {
	return p.transicion(ctx, mercadoID, "open", nil)
}

func (p *PagosHTTP) Cerrar(ctx context.Context, mercadoID string) error {
	return p.transicion(ctx, mercadoID, "close", nil)
}

func (p *PagosHTTP) Resolver(ctx context.Context, mercadoID, opcion string) error {
	return p.transicion(ctx, mercadoID, "resolve", map[string]string{"winning_option": opcion})
}

func (p *PagosHTTP) transicion(ctx context.Context, mercadoID, accion string, cuerpo any) error {
	estado, b, err := p.pedir(ctx, "/internal/markets/"+url.PathEscape(mercadoID)+"/"+accion, cuerpo)
	if err != nil {
		return err
	}
	return clasificar(accion+" "+mercadoID, estado, b)
}

// segundoCierre redondea hacia abajo: con race_close_s nadie apuesta despues
// del cierre real.
func segundoCierre(cierreMs int64) int64 {
	if cierreMs < 0 {
		return 0
	}
	return cierreMs / 1000
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

// pedir hace un POST con el token de servicio. Un fallo de red es reintentable.
func (p *PagosHTTP) pedir(ctx context.Context, ruta string, cuerpo any) (int, []byte, error) {
	var lector io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return 0, nil, err
		}
		lector = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+ruta, lector)
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
