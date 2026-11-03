package cierre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PagosHTTP lee de Pagos por REST con el token de servicio.
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

func (p *PagosHTTP) Evento(ctx context.Context, eventoID string) (Evento, error) {
	var e Evento
	return e, p.leer(ctx, "/internal/events/"+url.PathEscape(eventoID), &e)
}

func (p *PagosHTTP) Pendientes(ctx context.Context, eventoID string) (Pendientes, error) {
	var r Pendientes
	return r, p.leer(ctx, "/internal/events/"+url.PathEscape(eventoID)+"/pendientes", &r)
}

func (p *PagosHTTP) leer(ctx context.Context, ruta string, destino any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+ruta, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Service-Token", p.Token)
	resp, err := p.Cliente.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPagosNoDisponible, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GET %s respondio %d", ErrPagosNoDisponible, ruta, resp.StatusCode)
	}
	if err := json.Unmarshal(b, destino); err != nil {
		return fmt.Errorf("%w: respuesta invalida: %v", ErrPagosNoDisponible, err)
	}
	return nil
}
