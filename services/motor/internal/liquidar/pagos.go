package liquidar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PagosHTTP habla con Pagos por REST con el token de servicio.
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

func (p *PagosHTTP) MercadosPorLiquidar(ctx context.Context) ([]Mercado, error) {
	var ms []Mercado
	return ms, p.leer(ctx, "/internal/markets?status=cerrado&resuelto=true&limit=50", &ms)
}

func (p *PagosHTTP) ApuestasPendientes(ctx context.Context, mercadoID, modo string) ([]Apuesta, error) {
	var as []Apuesta
	ruta := "/internal/markets/" + url.PathEscape(mercadoID) + "/bets?mode=" + url.QueryEscape(modo) + "&status=pending"
	return as, p.leer(ctx, ruta, &as)
}

// Liquidar llama a POST /internal/settlements con la clave
// bet-settlement:{bet_id}. 201 = pagada ahora, 200 = ya estaba (reintento).
func (p *PagosHTTP) Liquidar(ctx context.Context, betID string, gano bool) (Pago, error) {
	resultado := "lost"
	if gano {
		resultado = "won"
	}
	clave := "bet-settlement:" + betID
	cuerpo := map[string]string{"bet_id": betID, "result": resultado, "idempotency_key": clave}
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/settlements", cuerpo, clave)
	if err != nil {
		return Pago{}, err
	}
	if estado != http.StatusCreated && estado != http.StatusOK {
		return Pago{}, fmt.Errorf("%w: settlement respondio %d: %s", ErrPagosNoDisponible, estado, b)
	}
	var r Pago
	if err := json.Unmarshal(b, &r); err != nil {
		return Pago{}, fmt.Errorf("%w: respuesta invalida: %v", ErrPagosNoDisponible, err)
	}
	return r, nil
}

// LiquidarTorneo llama a POST /internal/bets/torneo/{bet_id}/settlement: marca
// won/lost y el pago en fichas, sin tokens. 201 = ahora, 200 = ya estaba.
func (p *PagosHTTP) LiquidarTorneo(ctx context.Context, betID string, gano bool) (PagoTorneo, error) {
	resultado := "lost"
	if gano {
		resultado = "won"
	}
	ruta := "/internal/bets/torneo/" + url.PathEscape(betID) + "/settlement"
	estado, b, err := p.pedir(ctx, http.MethodPost, ruta, map[string]string{"result": resultado}, "")
	if err != nil {
		return PagoTorneo{}, err
	}
	if estado != http.StatusCreated && estado != http.StatusOK {
		return PagoTorneo{}, fmt.Errorf("%w: settlement de torneo respondio %d: %s", ErrPagosNoDisponible, estado, b)
	}
	var r PagoTorneo
	if err := json.Unmarshal(b, &r); err != nil {
		return PagoTorneo{}, fmt.Errorf("%w: respuesta invalida: %v", ErrPagosNoDisponible, err)
	}
	return r, nil
}

func (p *PagosHTTP) MarcarLiquidado(ctx context.Context, mercadoID string) error {
	estado, b, err := p.pedir(ctx, http.MethodPost, "/internal/markets/"+url.PathEscape(mercadoID)+"/liquidated", nil, "")
	if err != nil {
		return err
	}
	if estado != http.StatusOK {
		return fmt.Errorf("%w: liquidated respondio %d: %s", ErrPagosNoDisponible, estado, b)
	}
	return nil
}

func (p *PagosHTTP) leer(ctx context.Context, ruta string, destino any) error {
	estado, b, err := p.pedir(ctx, http.MethodGet, ruta, nil, "")
	if err != nil {
		return err
	}
	if estado != http.StatusOK {
		return fmt.Errorf("%w: GET %s respondio %d", ErrPagosNoDisponible, ruta, estado)
	}
	if err := json.Unmarshal(b, destino); err != nil {
		return fmt.Errorf("%w: respuesta invalida: %v", ErrPagosNoDisponible, err)
	}
	return nil
}

func (p *PagosHTTP) pedir(ctx context.Context, metodo, ruta string, cuerpo any, clave string) (int, []byte, error) {
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
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	resp, err := p.Cliente.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrPagosNoDisponible, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}
