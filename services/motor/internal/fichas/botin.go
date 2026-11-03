package fichas

import (
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

// PagosBotin es lo que el Difusor necesita de Pagos: el pot del evento, que
// va en el ranking (RF-18). Despues de la largada no cambia.
type PagosBotin interface {
	Botin(ctx context.Context, eventoID string) (int64, error)
}

// ErrBotinNoDisponible: Pagos no respondio o respondio algo inesperado. El
// Difusor no falla por esto: difunde con el ultimo valor conocido.
var ErrBotinNoDisponible = errors.New("pagos no disponible para leer el botin")

// PagosBotinHTTP llama a GET /internal/events/{id} de Pagos.
type PagosBotinHTTP struct {
	BaseURL string
	Token   string
	Cliente *http.Client
}

// NuevoPagosBotinHTTP usa un timeout corto: el ranking no espera a Pagos mas
// de eso (RNF-13).
func NuevoPagosBotinHTTP(baseURL, token string) *PagosBotinHTTP {
	return &PagosBotinHTTP{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Cliente: &http.Client{Timeout: 2 * time.Second},
	}
}

var _ PagosBotin = (*PagosBotinHTTP)(nil)

func (p *PagosBotinHTTP) Botin(ctx context.Context, eventoID string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/internal/events/"+url.PathEscape(eventoID), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Internal-Service-Token", p.Token)
	resp, err := p.Cliente.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrBotinNoDisponible, err)
	}
	defer resp.Body.Close()
	cuerpo := io.LimitReader(resp.Body, 1<<16)
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, cuerpo)
		return 0, fmt.Errorf("%w: respondio %d", ErrBotinNoDisponible, resp.StatusCode)
	}
	var ev struct {
		Pot *int64 `json:"pot"`
	}
	if err := json.NewDecoder(cuerpo).Decode(&ev); err != nil || ev.Pot == nil {
		return 0, fmt.Errorf("%w: respuesta sin pot", ErrBotinNoDisponible)
	}
	return *ev.Pot, nil
}
