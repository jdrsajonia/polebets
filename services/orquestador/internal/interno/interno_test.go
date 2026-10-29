package interno

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/traza"
)

const tokenPrueba = "token-interno-de-pruebas-orquestador"

// servidor responde con los estados de la lista, uno por llamada (el ultimo
// se repite), y cuenta cuantas llamadas recibio.
func servidor(t *testing.T, estados ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var llamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(llamadas.Add(1))
		w.WriteHeader(estados[min(n, len(estados))-1])
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &llamadas
}

// cliente usa esperas de 1 ms para que las pruebas corran rapido; el numero
// de intentos (3) es el mismo del contrato.
func cliente(baseURL string) *Cliente {
	c := Nuevo(baseURL, tokenPrueba)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c
}

func TestValoresDelContrato(t *testing.T) {
	c := Nuevo("http://pagos:8000", tokenPrueba)
	if c.HTTP.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, se esperaba 3s", c.HTTP.Timeout)
	}
	if len(c.Esperas) != 2 || c.Esperas[0] != 200*time.Millisecond || c.Esperas[1] != 400*time.Millisecond {
		t.Errorf("esperas = %v, se esperaba [200ms 400ms]", c.Esperas)
	}
}

func TestReintentos(t *testing.T) {
	casos := []struct {
		nombre   string
		peticion Peticion
		estados  []int
		llamadas int32
		estado   int  // estado devuelto si no hay error
		noDispo  bool // se espera ErrNoDisponible
	}{
		{"GET reintenta 503 y luego responde", Peticion{Metodo: "GET", Ruta: "/x"}, []int{503, 503, 200}, 3, 200, false},
		{"GET reintenta 502 y 504", Peticion{Metodo: "GET", Ruta: "/x"}, []int{502, 504, 200}, 3, 200, false},
		{"GET con 503 siempre se agota", Peticion{Metodo: "GET", Ruta: "/x"}, []int{503}, 3, 0, true},
		{"GET con 404 no reintenta", Peticion{Metodo: "GET", Ruta: "/x"}, []int{404, 200}, 1, 404, false},
		{"GET con 500 no reintenta", Peticion{Metodo: "GET", Ruta: "/x"}, []int{500, 200}, 1, 500, false},
		{"POST con clave reintenta 503", Peticion{Metodo: "POST", Ruta: "/x", IdempotencyKey: "k"}, []int{503, 201}, 2, 201, false},
		{"POST con clave y 409 no reintenta", Peticion{Metodo: "POST", Ruta: "/x", IdempotencyKey: "k"}, []int{409, 201}, 1, 409, false},
		{"POST con clave y 422 no reintenta", Peticion{Metodo: "POST", Ruta: "/x", IdempotencyKey: "k"}, []int{422, 201}, 1, 422, false},
		{"POST sin clave no reintenta 503", Peticion{Metodo: "POST", Ruta: "/x"}, []int{503, 201}, 1, 0, true},
		{"DELETE sin clave no reintenta 503", Peticion{Metodo: "DELETE", Ruta: "/x"}, []int{503, 204}, 1, 0, true},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			srv, llamadas := servidor(t, c.estados...)
			resp, err := cliente(srv.URL).Hacer(context.Background(), c.peticion)

			if got := llamadas.Load(); got != c.llamadas {
				t.Errorf("llamadas = %d, se esperaban %d", got, c.llamadas)
			}
			if c.noDispo {
				if !errors.Is(err, ErrNoDisponible) {
					t.Fatalf("err = %v, se esperaba ErrNoDisponible", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			if resp.Estado != c.estado {
				t.Errorf("estado = %d, se esperaba %d", resp.Estado, c.estado)
			}
			if string(resp.Cuerpo) != `{"ok":true}` {
				t.Errorf("cuerpo = %q", resp.Cuerpo)
			}
		})
	}
}

func TestRespetaElTimeout(t *testing.T) {
	liberar := make(chan struct{})
	var llamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamadas.Add(1)
		select { // mas lento que el timeout del cliente
		case <-liberar:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(liberar) }) // corre antes que srv.Close

	c := cliente(srv.URL)
	c.HTTP.Timeout = 50 * time.Millisecond

	inicio := time.Now()
	_, err := c.Hacer(context.Background(), Peticion{Metodo: "GET", Ruta: "/lento"})
	if !errors.Is(err, ErrNoDisponible) {
		t.Fatalf("err = %v, se esperaba ErrNoDisponible", err)
	}
	if got := llamadas.Load(); got != 3 {
		t.Errorf("llamadas = %d, se esperaban 3 (el timeout se reintenta en GET)", got)
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("tardo %v; el timeout por intento no se respeto", d)
	}
}

func TestErrorDeRedEsNoDisponible(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nadie escucha en esa direccion

	_, err := cliente(url).Hacer(context.Background(), Peticion{Metodo: "GET", Ruta: "/x"})
	if !errors.Is(err, ErrNoDisponible) {
		t.Fatalf("err = %v, se esperaba ErrNoDisponible", err)
	}
}

func TestCancelarCortaLosReintentos(t *testing.T) {
	srv, llamadas := servidor(t, 503)
	c := cliente(srv.URL)
	c.Esperas = []time.Duration{time.Minute, time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for llamadas.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel() // el jugador cerro la conexion durante la espera
	}()

	_, err := c.Hacer(ctx, Peticion{Metodo: "GET", Ruta: "/x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, se esperaba context.Canceled", err)
	}
	if got := llamadas.Load(); got != 1 {
		t.Errorf("llamadas = %d, se esperaba 1", got)
	}
}

func TestHeadersYCuerpo(t *testing.T) {
	type recibido struct {
		token, idPeticion, tipo, clave, cuerpo string
	}
	var vistos []recibido
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		vistos = append(vistos, recibido{
			r.Header.Get("X-Internal-Service-Token"),
			r.Header.Get("X-Request-Id"),
			r.Header.Get("Content-Type"),
			r.Header.Get("Idempotency-Key"),
			string(b),
		})
		if len(vistos) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	ctx := traza.EnContexto(context.Background(), "gw-1234")
	_, err := cliente(srv.URL).Hacer(ctx, Peticion{
		Metodo:         "POST",
		Ruta:           "/internal/wallets",
		Cuerpo:         map[string]string{"user_id": "u1"},
		IdempotencyKey: "wallet:u1",
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(vistos) != 2 {
		t.Fatalf("llamadas = %d, se esperaban 2", len(vistos))
	}
	for i, v := range vistos {
		if v.token != tokenPrueba || v.idPeticion != "gw-1234" || v.tipo != "application/json" || v.clave != "wallet:u1" {
			t.Errorf("intento %d: headers = %+v", i+1, v)
		}
		var cuerpo map[string]string
		if err := json.Unmarshal([]byte(v.cuerpo), &cuerpo); err != nil || cuerpo["user_id"] != "u1" {
			t.Errorf("intento %d: cuerpo = %q (el reintento debe reenviar el mismo cuerpo)", i+1, v.cuerpo)
		}
	}
}

func TestSinCuerpoNiClaveNoMandaEsosHeaders(t *testing.T) {
	var tipo, clave, idPeticion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tipo, clave, idPeticion = r.Header.Get("Content-Type"), r.Header.Get("Idempotency-Key"), r.Header.Get("X-Request-Id")
	}))
	t.Cleanup(srv.Close)

	if _, err := cliente(srv.URL).Hacer(context.Background(), Peticion{Metodo: "GET", Ruta: "/x"}); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if tipo != "" || clave != "" {
		t.Errorf("Content-Type = %q, Idempotency-Key = %q; se esperaban vacios", tipo, clave)
	}
	if len(idPeticion) != 32 {
		t.Errorf("X-Request-Id = %q, se esperaba uno generado de 32 caracteres", idPeticion)
	}
}
