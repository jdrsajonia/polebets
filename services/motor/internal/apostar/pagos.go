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
		// Pagos usa 409 para tres casos; los distingue el detalle.
		codigo, texto := detalle(cuerpo)
		switch {
		case codigo == "mercado_cerrado":
			return ResultadoDebito{}, ErrMercadoCerrado
		case strings.Contains(texto, "apost"):
			return ResultadoDebito{}, ErrYaApostaste
		}
		return ResultadoDebito{}, ErrClaveReutilizada
	case http.StatusUnprocessableEntity, http.StatusNotFound:
		return ResultadoDebito{}, ErrSaldoInsuficiente
	default:
		return ResultadoDebito{}, fmt.Errorf("%w: debito respondio %d: %s", ErrPagosNoDisponible, estado, cuerpo)
	}
}

// RegistrarTorneo llama a POST /internal/bets/torneo: guarda la apuesta en
// bets sin mover tokens. Idempotente por bet_id.
//
//	201/200 -> registrada (200 = ya estaba con los mismos datos)
//	409     -> mercado_cerrado, o ya_apostaste/datos_distintos: el bet_id de
//	           torneo sale de (usuario, mercado), asi que otro monto u opcion
//	           en el mismo mercado llega como datos_distintos. Las dos son
//	           "ya apostaste".
//	404     -> el mercado no existe
//	otro    -> Pagos no esta disponible
func (p *PagosHTTP) RegistrarTorneo(ctx context.Context, a ApuestaTorneo) (bool, error) {
	estado, cuerpo, err := p.pedir(ctx, http.MethodPost, "/internal/bets/torneo", a, "")
	if err != nil {
		return false, err
	}
	switch estado {
	case http.StatusCreated, http.StatusOK:
		return estado == http.StatusCreated, nil
	case http.StatusConflict:
		if codigo, _ := detalle(cuerpo); codigo == "mercado_cerrado" {
			return false, ErrMercadoCerrado
		}
		return false, ErrYaApostaste
	case http.StatusNotFound:
		return false, ErrMercadoNoExiste
	default:
		return false, fmt.Errorf("%w: registro de torneo respondio %d: %s", ErrPagosNoDisponible, estado, cuerpo)
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

// detalle lee el "detail" de un error de Pagos. Puede ser texto o un objeto
// {"codigo", "mensaje"}; devuelve el codigo (vacio si es texto) y el texto en
// minusculas.
func detalle(cuerpo []byte) (codigo, texto string) {
	var r struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(cuerpo, &r) != nil {
		return "", ""
	}
	var obj struct {
		Codigo  string `json:"codigo"`
		Mensaje string `json:"mensaje"`
	}
	if json.Unmarshal(r.Detail, &obj) == nil {
		return obj.Codigo, strings.ToLower(obj.Mensaje)
	}
	_ = json.Unmarshal(r.Detail, &texto)
	return "", strings.ToLower(texto)
}
