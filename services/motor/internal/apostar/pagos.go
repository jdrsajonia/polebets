package apostar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// Mercado lee GET /internal/markets/{id}.
func (p *PagosHTTP) Mercado(ctx context.Context, id string) (Mercado, error) {
	estado, cuerpo, err := p.pedir(ctx, http.MethodGet, "/internal/markets/"+id, nil, "")
	if err != nil {
		return Mercado{}, err
	}
	switch estado {
	case http.StatusOK:
		var m Mercado
		if err := json.Unmarshal(cuerpo, &m); err != nil {
			return Mercado{}, fmt.Errorf("%w: mercado invalido: %v", ErrPagosNoDisponible, err)
		}
		return m, nil
	case http.StatusNotFound:
		return Mercado{}, ErrMercadoNoExiste
	default:
		return Mercado{}, fmt.Errorf("%w: GET mercado respondio %d", ErrPagosNoDisponible, estado)
	}
}

// Debitar llama a POST /internal/debit.
//
//	201/200 -> debitado (200 = reenvio con la misma clave, no cobra de nuevo)
//	409     -> ya aposto en ese mercado, o clave reutilizada con otro contenido
//	422     -> saldo insuficiente
//	404     -> sin billetera (o mercado borrado): se trata como saldo insuficiente
//	otro    -> Pagos no esta disponible
func (p *PagosHTTP) Debitar(ctx context.Context, d Debito) (ResultadoDebito, error) {
	estado, cuerpo, err := p.pedir(ctx, http.MethodPost, "/internal/debit", d, d.IdempotencyKey)
	if err != nil {
		return ResultadoDebito{}, err
	}
	switch estado {
	case http.StatusCreated, http.StatusOK:
		var r struct {
			Saldo int64 `json:"saldo"`
			Nueva bool  `json:"nueva"`
		}
		if err := json.Unmarshal(cuerpo, &r); err != nil {
			return ResultadoDebito{}, fmt.Errorf("%w: respuesta invalida: %v", ErrPagosNoDisponible, err)
		}
		return ResultadoDebito{Saldo: r.Saldo, Nueva: r.Nueva}, nil
	case http.StatusConflict:
		// Pagos usa 409 para dos casos; los distingue el detalle.
		if strings.Contains(detalle(cuerpo), "apost") {
			return ResultadoDebito{}, ErrYaApostaste
		}
		return ResultadoDebito{}, ErrClaveReutilizada
	case http.StatusUnprocessableEntity, http.StatusNotFound:
		return ResultadoDebito{}, ErrSaldoInsuficiente
	default:
		return ResultadoDebito{}, fmt.Errorf("%w: debito respondio %d: %s", ErrPagosNoDisponible, estado, cuerpo)
	}
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

func detalle(cuerpo []byte) string {
	var r struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(cuerpo, &r)
	return strings.ToLower(r.Detail)
}
