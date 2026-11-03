package eventos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

const (
	evento  = "11111111-2222-4333-8444-555555555555"
	jugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"
	token   = "token-interno-de-pruebas-orquestador"
)

// pagosFalso responde según la ruta pedida, imitando a Pagos.
type pagosFalso struct {
	evento      string // cuerpo JSON para GET /internal/events/{id}
	eventoList  string // cuerpo para GET /internal/events?...
	mercados    string // cuerpo para .../markets
	inscripcion string // cuerpo para .../registrations/...
	estEvento   int
	estInscr    int
}

func nuevoCliente(t *testing.T, f *pagosFalso) *interno.Cliente {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/markets"):
			w.Write([]byte(f.mercados))
		case strings.Contains(p, "/registrations/"):
			w.WriteHeader(f.estInscr)
			w.Write([]byte(f.inscripcion))
		case strings.HasPrefix(p, "/internal/events/"):
			w.WriteHeader(f.estEvento)
			w.Write([]byte(f.evento))
		case p == "/internal/events":
			w.Write([]byte(f.eventoList))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c
}

func pedir(t *testing.T, pagos Servicio, ruta, rol string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, pagos)
	req := httptest.NewRequest(http.MethodGet, ruta, nil)
	if jugador != "" {
		req.Header.Set(sesion.HeaderUsuario, jugador)
	}
	if rol != "" {
		req.Header.Set(sesion.HeaderRol, rol)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestListarMapeaNombresPublicos(t *testing.T) {
	f := &pagosFalso{eventoList: `[
		{"event_id":"` + evento + `","race_key":"2024_monza","status":"programado",
		 "tournament_status":"abierto","starts_at":"2026-10-26T16:00:00Z","registered":3,
		 "pot":300,"buy_in":100,"initial_chips":1000}]`}
	rec := pedir(t, nuevoCliente(t, f), "/eventos", sesion.RolJugador)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200: %s", rec.Code, rec.Body)
	}
	var out ListaEventos
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("respuesta invalida: %v", err)
	}
	if len(out.Eventos) != 1 {
		t.Fatalf("eventos = %d, se esperaba 1", len(out.Eventos))
	}
	e := out.Eventos[0]
	if e.Carrera != "2024_monza" || e.Inscritos != 3 || e.Botin != 300 || e.CostoInscripcion != 100 {
		t.Errorf("mapeo incorrecto: %+v", e)
	}
	if e.Largada != "2026-10-26T16:00:00Z" || e.Estado != "programado" || e.EstadoTorneo != "abierto" {
		t.Errorf("mapeo incorrecto: %+v", e)
	}
}

func TestDetalleJuntaEventoMercadosEInscripcion(t *testing.T) {
	f := &pagosFalso{
		estEvento: http.StatusOK,
		evento: `{"event_id":"` + evento + `","race_key":"2024_monza","status":"programado",
			"tournament_status":"abierto","starts_at":"2026-10-26T16:00:00Z","registered":3,
			"pot":300,"buy_in":100,"initial_chips":1000}`,
		mercados: `[{"market_id":"m1","template":"ganador","question":"¿Quién gana?",
			"status":"abierto","closes_at":"2026-10-26T16:00:00Z",
			"options":[{"opcion":"VER","multiplicador":2.5}]}]`,
		estInscr:    http.StatusOK,
		inscripcion: `{"tournament_status":"abierto","refunded":false}`,
	}
	rec := pedir(t, nuevoCliente(t, f), "/eventos/"+evento, sesion.RolJugador)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200: %s", rec.Code, rec.Body)
	}
	var d DetalleEvento
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("respuesta invalida: %v", err)
	}
	if d.Evento.Carrera != "2024_monza" {
		t.Errorf("evento mal mapeado: %+v", d.Evento)
	}
	if len(d.MercadosEstaticos) != 1 || d.MercadosEstaticos[0].Plantilla != "ganador" {
		t.Fatalf("mercados = %+v", d.MercadosEstaticos)
	}
	if d.MercadosEstaticos[0].Opciones[0].Opcion != "VER" || d.MercadosEstaticos[0].Opciones[0].Multiplicador != 2.5 {
		t.Errorf("opcion mal mapeada: %+v", d.MercadosEstaticos[0].Opciones)
	}
	if !d.MiInscripcion.Inscrito || d.MiInscripcion.Devuelta {
		t.Errorf("mi_inscripcion = %+v, se esperaba inscrito sin devolucion", d.MiInscripcion)
	}
}

func TestDetalleNoInscritoCuando404(t *testing.T) {
	f := &pagosFalso{
		estEvento:   http.StatusOK,
		evento:      `{"event_id":"` + evento + `","race_key":"x","status":"programado","tournament_status":"abierto","starts_at":"t","registered":0,"pot":0,"buy_in":100,"initial_chips":1000}`,
		mercados:    `[]`,
		estInscr:    http.StatusNotFound,
		inscripcion: `{}`,
	}
	rec := pedir(t, nuevoCliente(t, f), "/eventos/"+evento, sesion.RolJugador)

	var d DetalleEvento
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.MiInscripcion.Inscrito {
		t.Errorf("con 404 en registrations el jugador NO debe salir inscrito")
	}
}

func TestEventoInexistenteDa404(t *testing.T) {
	f := &pagosFalso{estEvento: http.StatusNotFound, evento: `{}`, mercados: `[]`, estInscr: http.StatusNotFound}
	rec := pedir(t, nuevoCliente(t, f), "/eventos/"+evento, sesion.RolJugador)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, se esperaba 404", rec.Code)
	}
	var cuerpo map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &cuerpo)
	if cuerpo["codigo"] != "evento_no_existe" {
		t.Errorf("codigo = %q, se esperaba evento_no_existe", cuerpo["codigo"])
	}
}

func TestSinSesionDa401(t *testing.T) {
	f := &pagosFalso{eventoList: `[]`}
	rec := pedir(t, nuevoCliente(t, f), "/eventos", "") // sin rol
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, se esperaba 401", rec.Code)
	}
}

func TestAdminNoEntra(t *testing.T) {
	f := &pagosFalso{eventoList: `[]`}
	rec := pedir(t, nuevoCliente(t, f), "/eventos", sesion.RolAdmin)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, se esperaba 403", rec.Code)
	}
}
