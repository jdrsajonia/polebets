package errores

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arquisoft/orquestador/internal/interno"
)

// TestTablaDelContrato fija los pares (estado, codigo) de la seccion 2 del
// contrato: si alguien cambia uno aqui, la prueba obliga a cambiar el doc.
func TestTablaDelContrato(t *testing.T) {
	casos := []struct {
		err    Error
		estado int
		codigo string
	}{
		{CuerpoInvalido, 400, "cuerpo_invalido"},
		{FaltaIdempotencyKey, 400, "falta_idempotency_key"},
		{SinSesion, 401, "sin_sesion"},
		{SoloJugadores, 403, "solo_jugadores"},
		{EventoNoExiste, 404, "evento_no_existe"},
		{MercadoNoExiste, 404, "mercado_no_existe"},
		{CorreoRegistrado, 409, "correo_registrado"},
		{InscripcionesCerradas, 409, "inscripciones_cerradas"},
		{MercadoCerrado, 409, "mercado_cerrado"},
		{YaApostaste, 409, "ya_apostaste"},
		{ClaveReutilizada, 409, "clave_reutilizada"},
		{SaldoInsuficiente, 422, "saldo_insuficiente"},
		{FichasInsuficientes, 422, "fichas_insuficientes"},
		{MontoInvalido, 422, "monto_invalido"},
		{OpcionInvalida, 422, "opcion_invalida"},
		{ServicioNoDisponible, 503, "servicio_no_disponible"},
		{Interno, 500, "error_interno"},
	}
	vistos := map[string]bool{}
	for _, c := range casos {
		if c.err.Estado != c.estado || c.err.Codigo != c.codigo {
			t.Errorf("%s: (%d, %s), se esperaba (%d, %s)", c.codigo, c.err.Estado, c.err.Codigo, c.estado, c.codigo)
		}
		if c.err.Mensaje == "" {
			t.Errorf("%s: sin mensaje", c.codigo)
		}
		if vistos[c.err.Codigo] {
			t.Errorf("codigo repetido: %s", c.err.Codigo)
		}
		vistos[c.err.Codigo] = true
	}
}

func responder(t *testing.T, err error) (int, Cuerpo) {
	t.Helper()
	rec := httptest.NewRecorder()
	Responder(rec, err)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var c Cuerpo
	if err := json.NewDecoder(rec.Body).Decode(&c); err != nil {
		t.Fatalf("cuerpo no es JSON: %v", err)
	}
	return rec.Code, c
}

func TestResponderErrorDeLaTabla(t *testing.T) {
	estado, c := responder(t, MercadoCerrado)
	if estado != http.StatusConflict || c.Codigo != "mercado_cerrado" || c.Error != "Mercado cerrado" {
		t.Errorf("respuesta = %d %+v", estado, c)
	}
}

func TestResponderErrorEnvuelto(t *testing.T) {
	err := fmt.Errorf("motor respondio 409: %w", MercadoCerrado)
	if !errors.Is(err, MercadoCerrado) {
		t.Fatal("errors.Is deberia reconocer el error envuelto")
	}
	estado, c := responder(t, err)
	if estado != http.StatusConflict || c.Codigo != "mercado_cerrado" {
		t.Errorf("respuesta = %d %+v", estado, c)
	}
}

func TestResponderServicioNoDisponibleEs503(t *testing.T) {
	err := fmt.Errorf("%w: POST /internal/wallets tras 3 intento(s): respondio 503", interno.ErrNoDisponible)
	estado, c := responder(t, err)
	if estado != http.StatusServiceUnavailable || c.Codigo != "servicio_no_disponible" || c.Error != ServicioNoDisponible.Mensaje {
		t.Errorf("respuesta = %d %+v", estado, c)
	}
}

func TestResponderErrorDesconocidoEs500SinDetalle(t *testing.T) {
	estado, c := responder(t, errors.New("pq: connection refused en 10.0.0.5"))
	if estado != http.StatusInternalServerError || c.Codigo != "error_interno" || c.Error != "Error interno" {
		t.Errorf("respuesta = %d %+v", estado, c)
	}
}
