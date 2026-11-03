package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Registro es una fila tal cual la devuelve OpenF1.
type Registro map[string]any

// clienteOpenF1 hace las peticiones a la API pública de OpenF1 (sistema externo).
type clienteOpenF1 struct {
	base       string
	apiKey     string
	http       *http.Client
	pausa      time.Duration // espacio mínimo entre peticiones
	reintentos int
	ultima     time.Time
}

func nuevoClienteOpenF1(cfg Config) *clienteOpenF1 {
	return &clienteOpenF1{
		base:       cfg.OpenF1URL,
		apiKey:     cfg.OpenF1APIKey,
		http:       &http.Client{Timeout: 90 * time.Second},
		pausa:      400 * time.Millisecond,
		reintentos: 6,
	}
}

// errorHTTP es una respuesta que no se debe reintentar (4xx distinto de 429).
type errorHTTP struct {
	estado int
	url    string
	cuerpo string
}

func (e *errorHTTP) Error() string {
	return fmt.Sprintf("OpenF1 respondió %d para %s: %s", e.estado, e.url, e.cuerpo)
}

// obtener pide un endpoint con sus filtros ("session_key=9165", "date>=...")
// y devuelve las filas. Un 404 de OpenF1
// significa "No results found." (no hay datos para ese filtro), no un error.
// Los 429, 5xx y fallos de red se reintentan con espera creciente.
func (c *clienteOpenF1) obtener(ctx context.Context, endpoint string, filtros ...string) ([]Registro, error) {
	// Los filtros van literales: OpenF1 pone el operador en la clave (date>=...)
	// y url.Values lo escaparía.
	destino := c.base + "/" + endpoint + "?" + strings.Join(filtros, "&")

	var ultimoErr error
	for intento := 0; intento < c.reintentos; intento++ {
		if intento > 0 {
			espera := esperaReintento(intento, ultimoErr)
			log.Printf("↻ %s: reintento %d/%d en %v (%v)", endpoint, intento, c.reintentos-1, espera, ultimoErr)
			if err := dormir(ctx, espera); err != nil {
				return nil, err
			}
		}
		if err := dormir(ctx, time.Until(c.ultima.Add(c.pausa))); err != nil {
			return nil, err
		}
		c.ultima = time.Now()

		filas, err := c.pedir(ctx, destino)
		if err == nil {
			return filas, nil
		}
		var noReintentable *errorHTTP
		if errors.As(err, &noReintentable) {
			return nil, err
		}
		ultimoErr = err
	}
	return nil, fmt.Errorf("%s: sin respuesta válida tras %d intentos: %w", endpoint, c.reintentos, ultimoErr)
}

// errorReintentable lleva la espera que pidió el servidor en Retry-After.
type errorReintentable struct {
	causa      string
	retryAfter time.Duration
}

func (e *errorReintentable) Error() string { return e.causa }

func (c *clienteOpenF1) pedir(ctx context.Context, destino string) ([]Registro, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, destino, nil)
	if err != nil {
		return nil, &errorHTTP{url: destino, cuerpo: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	// El histórico de OpenF1 es gratis sin token: solo se manda si existe.
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &errorReintentable{causa: err.Error()}
	}
	defer resp.Body.Close()

	cuerpo, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &errorReintentable{causa: "leyendo la respuesta: " + err.Error()}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		var filas []Registro
		if err := json.Unmarshal(cuerpo, &filas); err != nil {
			return nil, &errorHTTP{estado: resp.StatusCode, url: destino, cuerpo: "JSON inválido: " + err.Error()}
		}
		return filas, nil
	case resp.StatusCode == http.StatusNotFound:
		return []Registro{}, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, &errorReintentable{
			causa:      fmt.Sprintf("estado %d", resp.StatusCode),
			retryAfter: leerRetryAfter(resp.Header.Get("Retry-After")),
		}
	default:
		return nil, &errorHTTP{estado: resp.StatusCode, url: destino, cuerpo: recortar(string(cuerpo), 200)}
	}
}

// esperaReintento: 2, 4, 8, 16, 32 s, o lo que pida Retry-After si es mayor.
func esperaReintento(intento int, ultimoErr error) time.Duration {
	espera := time.Duration(1<<intento) * time.Second
	var r *errorReintentable
	if errors.As(ultimoErr, &r) && r.retryAfter > espera {
		espera = r.retryAfter
	}
	return espera
}

func leerRetryAfter(v string) time.Duration {
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

func dormir(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func recortar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
