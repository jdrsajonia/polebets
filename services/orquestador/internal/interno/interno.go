// Package interno es el cliente HTTP comun del orquestador hacia Usuarios,
// Pagos y Motor (seccion 4 de documentation/orquestador-api.md). Los clientes
// de cada servicio lo usan por debajo y traducen sus respuestas a la tabla de
// errores de la API publica.
//
// Cada llamada lleva:
//
//	X-Internal-Service-Token  token de servicio (INTERNAL_API_TOKEN)
//	X-Request-Id              el de la peticion del jugador, o uno nuevo
//	Content-Type              application/json, si hay cuerpo
//	Idempotency-Key           solo si el flujo la pasa
//
// Reintentos: hasta 3 intentos (esperas de 200 ms y 400 ms) SOLO si la
// llamada es idempotente (GET o con Idempotency-Key) y el intento fallo por
// red, timeout o 502/503/504. Un 4xx nunca se reintenta: repetir la misma
// peticion daria la misma respuesta. Un POST sin clave tampoco: si el primer
// intento si llego al servicio, repetirlo podria crear algo dos veces.
package interno

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/arquisoft/orquestador/internal/traza"
)

// ErrNoDisponible indica que el servicio no respondio tras los reintentos.
// La API publica lo traduce a 503 servicio_no_disponible.
var ErrNoDisponible = errors.New("servicio no disponible")

const (
	// TimeoutPorIntento limita cada intento (conexion, envio y lectura).
	TimeoutPorIntento = 3 * time.Second
	// MaximoRespuesta es lo maximo que se lee del cuerpo de una respuesta.
	MaximoRespuesta = 1 << 20
)

// Peticion es una llamada a un servicio interno.
type Peticion struct {
	Metodo         string
	Ruta           string // se concatena a BaseURL, p. ej. "/internal/wallets"
	Cuerpo         any    // se envia como JSON; nil = sin cuerpo
	IdempotencyKey string // vacia = la llamada no es idempotente (salvo GET)
}

// Respuesta es lo que devolvio el servicio. Los 2xx, 4xx y 500 llegan aqui
// tal cual; el cliente de cada servicio decide que significan.
type Respuesta struct {
	Estado int
	Cuerpo []byte
}

// Cliente llama a un servicio interno.
type Cliente struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// Esperas entre intentos; len(Esperas)+1 es el numero maximo de intentos.
	Esperas []time.Duration
}

// Nuevo arma un cliente con el timeout y las esperas del contrato.
func Nuevo(baseURL, token string) *Cliente {
	return &Cliente{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: TimeoutPorIntento},
		Esperas: []time.Duration{200 * time.Millisecond, 400 * time.Millisecond},
	}
}

// Hacer envia la peticion con los reintentos que correspondan. Devuelve un
// error que envuelve ErrNoDisponible si el servicio no respondio, y el error
// del contexto si el jugador cancelo la peticion.
func (c *Cliente) Hacer(ctx context.Context, p Peticion) (Respuesta, error) {
	var cuerpo []byte
	if p.Cuerpo != nil {
		var err error
		if cuerpo, err = json.Marshal(p.Cuerpo); err != nil {
			return Respuesta{}, fmt.Errorf("serializar cuerpo de %s %s: %w", p.Metodo, p.Ruta, err)
		}
	}

	// El mismo id en todos los intentos, para verlos juntos en los logs.
	idPeticion := traza.De(ctx)
	if idPeticion == "" {
		idPeticion = traza.Nuevo()
	}

	intentos := 1
	if p.Metodo == http.MethodGet || p.IdempotencyKey != "" {
		intentos = len(c.Esperas) + 1
	}

	var ultimo error
	for i := 0; i < intentos; i++ {
		if i > 0 {
			if err := esperar(ctx, c.Esperas[i-1]); err != nil {
				return Respuesta{}, err
			}
		}

		resp, err := c.intento(ctx, p, cuerpo, idPeticion)
		switch {
		case err != nil && ctx.Err() != nil:
			return Respuesta{}, ctx.Err() // el jugador se fue: no se sigue intentando
		case err != nil:
			ultimo = err
		case reintentable(resp.Estado):
			ultimo = fmt.Errorf("respondio %d", resp.Estado)
		default:
			return resp, nil
		}
	}
	return Respuesta{}, fmt.Errorf("%w: %s %s tras %d intento(s): %v", ErrNoDisponible, p.Metodo, p.Ruta, intentos, ultimo)
}

func (c *Cliente) intento(ctx context.Context, p Peticion, cuerpo []byte, idPeticion string) (Respuesta, error) {
	var lector io.Reader
	if cuerpo != nil {
		lector = bytes.NewReader(cuerpo)
	}
	req, err := http.NewRequestWithContext(ctx, p.Metodo, c.BaseURL+p.Ruta, lector)
	if err != nil {
		return Respuesta{}, err
	}
	req.Header.Set("X-Internal-Service-Token", c.Token)
	req.Header.Set(traza.Header, idPeticion)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", p.IdempotencyKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Respuesta{}, err
	}
	defer resp.Body.Close()

	// El timeout del cliente cubre tambien la lectura del cuerpo.
	datos, err := io.ReadAll(io.LimitReader(resp.Body, MaximoRespuesta))
	if err != nil {
		return Respuesta{}, err
	}
	return Respuesta{Estado: resp.StatusCode, Cuerpo: datos}, nil
}

// reintentable: el servicio (o un proxy delante) dice que no pudo atender
// ahora, pero podria en un momento. El 500 no entra: suele ser un error del
// servicio que se repetiria igual.
func reintentable(estado int) bool {
	return estado == http.StatusBadGateway ||
		estado == http.StatusServiceUnavailable ||
		estado == http.StatusGatewayTimeout
}

func esperar(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
