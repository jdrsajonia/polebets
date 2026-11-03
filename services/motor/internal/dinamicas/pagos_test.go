package dinamicas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const tokenPrueba = "token-interno-de-pruebas-del-motor-32c"

// peticion es lo que el servidor falso vio.
type peticion struct {
	metodo, ruta, token, tipo string
	cuerpo                    map[string]any
}

func servidorPagos(t *testing.T, estado int, respuesta string) (*PagosHTTP, *peticion) {
	t.Helper()
	vista := &peticion{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vista.metodo, vista.ruta = r.Method, r.URL.Path
		vista.token, vista.tipo = r.Header.Get("X-Internal-Service-Token"), r.Header.Get("Content-Type")
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &vista.cuerpo); err != nil {
				t.Errorf("cuerpo no es JSON: %s", b)
			}
		}
		w.WriteHeader(estado)
		_, _ = io.WriteString(w, respuesta)
	}))
	t.Cleanup(srv.Close)
	return NuevoPagosHTTP(srv.URL+"/", tokenPrueba), vista
}

func altaPrueba() Alta {
	m := Mercado{
		Propuesta: Propuesta{
			Plantilla: PlantillaAdelantamiento,
			Pregunta:  "¿LEC irá delante de NOR al terminar el próximo minuto?",
			Opciones:  siNo(3.0, 1.3),
			Pilotos:   []int{4, 16},
		},
		ID:       IDMercado("11111111-2222-3333-4444-555555555555", PlantillaAdelantamiento, 100500),
		EventoID: "11111111-2222-3333-4444-555555555555",
		T0:       100500, Cierre: 130500, Resolucion: 190500,
	}
	bogota := time.FixedZone("COT", -5*3600)
	return Alta{
		Mercado:  m,
		Estado:   EstadoAbierto,
		AbreEn:   time.Date(2026, 11, 2, 15, 0, 0, 500_000_000, bogota),
		CierraEn: time.Date(2026, 11, 2, 15, 0, 30, 500_000_000, bogota),
	}
}

func TestCrearCuerpo(t *testing.T) {
	p, vista := servidorPagos(t, http.StatusCreated, `{}`)
	nuevo, err := p.Crear(context.Background(), altaPrueba())
	if err != nil || !nuevo {
		t.Fatalf("nuevo = %v, err = %v", nuevo, err)
	}
	if vista.metodo != http.MethodPost || vista.ruta != "/internal/markets" || vista.token != tokenPrueba || vista.tipo != "application/json" {
		t.Fatalf("peticion = %+v", vista)
	}
	c := vista.cuerpo
	a := altaPrueba()
	esperado := map[string]any{
		"market_id":    a.Mercado.ID,
		"event_id":     a.Mercado.EventoID,
		"kind":         "dinamica",
		"template":     "adelantamiento",
		"question":     a.Mercado.Pregunta,
		"status":       "abierto",
		"opens_at":     "2026-11-02T20:00:00.5Z", // en UTC, con zona
		"closes_at":    "2026-11-02T20:00:30.5Z",
		"race_close_s": 130.0, // 130500 ms hacia abajo
	}
	for k, v := range esperado {
		if c[k] != v {
			t.Errorf("%s = %v, se esperaba %v", k, c[k], v)
		}
	}
	opciones, _ := json.Marshal(c["options"])
	if string(opciones) != `[{"multiplicador":3,"opcion":"Sí"},{"multiplicador":1.3,"opcion":"No"}]` {
		t.Errorf("options = %s", opciones)
	}
}

func TestCrearRespuestas(t *testing.T) {
	casos := []struct {
		nombre string
		estado int
		nuevo  bool
		err    error
	}{
		{"201 creado", http.StatusCreated, true, nil},
		{"200 ya existia igual", http.StatusOK, false, nil},
		{"404 evento aun no existe", http.StatusNotFound, false, ErrReintentable},
		{"409 otros datos", http.StatusConflict, false, ErrConflicto},
		{"422 validacion", http.StatusUnprocessableEntity, false, ErrRechazado},
		{"401 token", http.StatusUnauthorized, false, ErrRechazado},
		{"500", http.StatusInternalServerError, false, ErrReintentable},
		{"503", http.StatusServiceUnavailable, false, ErrReintentable},
		{"429", http.StatusTooManyRequests, false, ErrReintentable},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			p, _ := servidorPagos(t, c.estado, `{"detail":"x"}`)
			nuevo, err := p.Crear(context.Background(), altaPrueba())
			if nuevo != c.nuevo || !errors.Is(err, c.err) || (c.err == nil) != (err == nil) {
				t.Fatalf("nuevo = %v, err = %v", nuevo, err)
			}
		})
	}
}

func TestTransiciones(t *testing.T) {
	const id = "0f8e6c2a-1b3d-5e7f-9a0b-1c2d3e4f5a6b"
	ops := []struct {
		nombre string
		ruta   string
		llamar func(*PagosHTTP) error
		cuerpo map[string]any
	}{
		{"abrir", "/internal/markets/" + id + "/open", func(p *PagosHTTP) error { return p.Abrir(context.Background(), id) }, nil},
		{"cerrar", "/internal/markets/" + id + "/close", func(p *PagosHTTP) error { return p.Cerrar(context.Background(), id) }, nil},
		{"resolver", "/internal/markets/" + id + "/resolve", func(p *PagosHTTP) error { return p.Resolver(context.Background(), id, OpcionSi) },
			map[string]any{"winning_option": "Sí"}},
	}
	estados := []struct {
		estado int
		err    error
	}{
		{http.StatusOK, nil},
		{http.StatusConflict, ErrConflicto},
		{http.StatusNotFound, ErrRechazado}, // al abrir/cerrar/resolver, 404 es el mercado
		{http.StatusUnprocessableEntity, ErrRechazado},
		{http.StatusBadGateway, ErrReintentable},
	}
	for _, op := range ops {
		for _, e := range estados {
			t.Run(op.nombre+" "+http.StatusText(e.estado), func(t *testing.T) {
				p, vista := servidorPagos(t, e.estado, `{}`)
				err := op.llamar(p)
				if !errors.Is(err, e.err) || (e.err == nil) != (err == nil) {
					t.Fatalf("err = %v", err)
				}
				if vista.metodo != http.MethodPost || vista.ruta != op.ruta || vista.token != tokenPrueba {
					t.Fatalf("peticion = %+v", vista)
				}
				if len(op.cuerpo) != len(vista.cuerpo) || (op.cuerpo != nil && vista.cuerpo["winning_option"] != op.cuerpo["winning_option"]) {
					t.Fatalf("cuerpo = %v", vista.cuerpo)
				}
			})
		}
	}
}

func TestPagosCaidoEsReintentable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	p := NuevoPagosHTTP(url, tokenPrueba)
	if _, err := p.Crear(context.Background(), altaPrueba()); !errors.Is(err, ErrReintentable) {
		t.Fatalf("crear: err = %v", err)
	}
	if err := p.Cerrar(context.Background(), "x"); !errors.Is(err, ErrReintentable) {
		t.Fatalf("cerrar: err = %v", err)
	}
}

func TestSegundoCierre(t *testing.T) {
	for ms, s := range map[int64]int64{130500: 130, 130000: 130, 999: 0, -5: 0} {
		if got := segundoCierre(ms); got != s {
			t.Errorf("segundoCierre(%d) = %d", ms, got)
		}
	}
}
