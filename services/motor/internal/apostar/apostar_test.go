package apostar

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const (
	token   = "token-interno-de-pruebas-del-motor-32c"
	jugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"
	mercado = "11111111-2222-4333-8444-555555555555"
	evento  = "99999999-8888-4777-8666-555555555555"
)

// pagosFalso simula Pagos: sirve el mercado y responde el debito con el
// estado configurado. Guarda el ultimo debito recibido.
type pagosFalso struct {
	mu            sync.Mutex
	mercadoEstado int
	mercado       string
	debitoEstado  int
	debitoCuerpo  string
	debito        Debito
	clave         string
	debitos       int
}

func (f *pagosFalso) servidor(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-Internal-Service-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /internal/markets/" + mercado:
			w.WriteHeader(f.mercadoEstado)
			_, _ = w.Write([]byte(f.mercado))
		case "POST /internal/debit":
			f.debitos++
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &f.debito)
			f.clave = r.Header.Get("Idempotency-Key")
			w.WriteHeader(f.debitoEstado)
			_, _ = w.Write([]byte(f.debitoCuerpo))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mercadoJSON(tipo, estado string, cierra *time.Time) string {
	m := map[string]any{
		"market_id": mercado, "event_id": evento, "kind": tipo, "status": estado,
		"options":   []map[string]any{{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3}},
		"closes_at": cierra,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func nuevoFalso() *pagosFalso {
	return &pagosFalso{
		mercadoEstado: http.StatusOK,
		mercado:       mercadoJSON(TipoEstatica, EstadoAbierto, nil),
		debitoEstado:  http.StatusCreated,
		debitoCuerpo:  `{"saldo":300,"nueva":true}`,
	}
}

func apostar(t *testing.T, f *pagosFalso, cuerpo, clave, tok string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	srv := f.servidor(t)
	svc := NuevoServicio(NuevoPagosHTTP(srv.URL, token), 500)
	mux := http.NewServeMux()
	Register(mux, svc, token)
	req := httptest.NewRequest(http.MethodPost, "/internal/apuestas", bytes.NewBufferString(cuerpo))
	if tok != "" {
		req.Header.Set("X-Internal-Service-Token", tok)
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func cuerpo(monto int, opcion string) string {
	b, _ := json.Marshal(Peticion{UserID: jugador, MercadoID: mercado, Opcion: opcion, Monto: int64(monto)})
	return string(b)
}

func TestApuestaIndividualDebitaEnPagosConElMultiplicador(t *testing.T) {
	f := nuevoFalso()

	rec, out := apostar(t, f, cuerpo(200, "VER"), "clave-1", token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("estado = %d, %s", rec.Code, rec.Body)
	}
	if out["modo"] != "individual" || out["multiplicador"] != 2.5 || out["pago_si_acierta"] != 500.0 || out["saldo"] != 300.0 || out["fichas"] != nil {
		t.Errorf("respuesta = %v", out)
	}
	d := f.debito
	if d.UserID != jugador || d.MarketID != mercado || d.Option != "VER" || d.Amount != 200 || d.Multiplier != "2.5" {
		t.Errorf("debito = %+v", d)
	}
	if f.clave != "bet-debit:"+d.BetID || d.IdempotencyKey != f.clave || out["apuesta_id"] != d.BetID {
		t.Errorf("clave %q, debito %+v", f.clave, d)
	}
}

func TestLaMismaClaveDaLaMismaApuesta(t *testing.T) {
	a := idDeterminista(jugador, "clave-1")
	if a != idDeterminista(jugador, "clave-1") || a == idDeterminista(jugador, "clave-2") || !esUUID(a) {
		t.Fatalf("ids: %s", a)
	}
	f := nuevoFalso()
	f.debitoEstado, f.debitoCuerpo = http.StatusOK, `{"saldo":300,"nueva":false}`
	rec, out := apostar(t, f, cuerpo(200, "VER"), "clave-1", token)
	if rec.Code != http.StatusOK || out["nueva"] != false || out["apuesta_id"] != a {
		t.Fatalf("reenvio = %d %v", rec.Code, out)
	}
}

func TestPagoSiAciertaTruncaComoPagos(t *testing.T) {
	f := nuevoFalso()
	f.mercado = `{"market_id":"` + mercado + `","event_id":"` + evento + `","kind":"estatica","status":"abierto","options":[{"opcion":"VER","multiplicador":1.33}]}`
	_, out := apostar(t, f, cuerpo(10, "VER"), "k", token)
	if out["pago_si_acierta"] != 13.0 { // 10 x 1.33 = 13.3 -> 13
		t.Fatalf("pago = %v", out["pago_si_acierta"])
	}
}

func TestErroresDeLaApuesta(t *testing.T) {
	ayer := time.Now().Add(-time.Hour)
	casos := []struct {
		nombre string
		ajuste func(f *pagosFalso)
		cuerpo string
		estado int
		codigo string
	}{
		{"tope individual 500", nil, cuerpo(501, "VER"), 422, "monto_invalido"},
		{"monto 0", nil, cuerpo(0, "VER"), 422, "monto_invalido"},
		{"opcion que no existe", nil, cuerpo(10, "HAM"), 422, "opcion_invalida"},
		{"mercado cerrado", func(f *pagosFalso) { f.mercado = mercadoJSON(TipoEstatica, "cerrado", nil) }, cuerpo(10, "VER"), 409, "mercado_cerrado"},
		{"mercado vencido", func(f *pagosFalso) { f.mercado = mercadoJSON(TipoEstatica, EstadoAbierto, &ayer) }, cuerpo(10, "VER"), 409, "mercado_cerrado"},
		{"mercado inexistente", func(f *pagosFalso) { f.mercadoEstado = 404 }, cuerpo(10, "VER"), 404, "mercado_no_existe"},
		{"ya aposto", func(f *pagosFalso) { f.debitoEstado, f.debitoCuerpo = 409, `{"detail":"Ya apostó en este mercado"}` }, cuerpo(10, "VER"), 409, "ya_apostaste"},
		{"clave reutilizada", func(f *pagosFalso) { f.debitoEstado, f.debitoCuerpo = 409, `{"detail":"Clave reutilizada"}` }, cuerpo(10, "VER"), 409, "clave_reutilizada"},
		{"saldo insuficiente", func(f *pagosFalso) { f.debitoEstado, f.debitoCuerpo = 422, `{"detail":"Saldo insuficiente"}` }, cuerpo(10, "VER"), 422, "saldo_insuficiente"},
		{"pagos caido", func(f *pagosFalso) { f.debitoEstado = 500 }, cuerpo(10, "VER"), 503, "servicio_no_disponible"},
		{"cuerpo con campos de mas", nil, `{"user_id":"` + jugador + `","mercado_id":"` + mercado + `","opcion":"VER","monto":1,"x":1}`, 400, "cuerpo_invalido"},
	}
	for _, c := range casos {
		f := nuevoFalso()
		if c.ajuste != nil {
			c.ajuste(f)
		}
		rec, out := apostar(t, f, c.cuerpo, "k", token)
		if rec.Code != c.estado || out["codigo"] != c.codigo {
			t.Errorf("%s: %d %v, se esperaba %d %s", c.nombre, rec.Code, out["codigo"], c.estado, c.codigo)
		}
	}
}

func TestSinTokenOSinClaveNoLlegaAPagos(t *testing.T) {
	f := nuevoFalso()
	if rec, _ := apostar(t, f, cuerpo(10, "VER"), "k", "otro-token"); rec.Code != http.StatusUnauthorized {
		t.Errorf("sin token = %d", rec.Code)
	}
	if rec, out := apostar(t, f, cuerpo(10, "VER"), "", token); rec.Code != http.StatusBadRequest || out["codigo"] != "falta_idempotency_key" {
		t.Errorf("sin clave = %d %v", rec.Code, out)
	}
	if f.debitos != 0 {
		t.Errorf("no debia debitar y debito %d veces", f.debitos)
	}
}

func TestDinamicaSinFichasEsIndividualYConFichasEsTorneo(t *testing.T) {
	svc := &Servicio{MaxIndividual: 500}
	m := Mercado{Tipo: "dinamica", EventoID: evento}
	if modo, _ := svc.modo(t.Context(), m, jugador); modo != ModoIndividual {
		t.Errorf("sin almacen de fichas = %s", modo)
	}
	svc.Fichas = fichasFalsas(true)
	if modo, _ := svc.modo(t.Context(), m, jugador); modo != ModoTorneo {
		t.Errorf("inscrito con fichas = %s", modo)
	}
	if modo, _ := svc.modo(t.Context(), Mercado{Tipo: TipoEstatica}, jugador); modo != ModoIndividual {
		t.Errorf("estatica = %s", modo)
	}
}

type fichasFalsas bool

func (f fichasFalsas) TieneFichas(_ context.Context, _, _ string) (bool, error) {
	return bool(f), nil
}
