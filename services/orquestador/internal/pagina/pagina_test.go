package pagina

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

const (
	jugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"
	token   = "token-interno-de-pruebas-orquestador"
	activo  = "11111111-2222-4333-8444-555555555555"
	cerrado = "22222222-2222-4333-8444-555555555555"
	cancel  = "33333333-2222-4333-8444-555555555555"
)

// falso responde segun la ruta (con query) y cuenta las llamadas. Una ruta que
// no esta en el mapa responde 404.
type falso struct {
	mu       sync.Mutex
	rutas    map[string]respuesta
	llamadas []string
}

type respuesta struct {
	estado int
	cuerpo string
}

func servicio(t *testing.T, rutas map[string]respuesta) (*interno.Cliente, *falso) {
	t.Helper()
	f := &falso{rutas: rutas}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		ruta := r.URL.RequestURI()
		f.llamadas = append(f.llamadas, ruta)
		if r.Header.Get("X-Internal-Service-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp, ok := f.rutas[ruta]
		if !ok {
			resp = respuesta{http.StatusNotFound, `{"detail":"no"}`}
		}
		w.WriteHeader(resp.estado)
		_, _ = w.Write([]byte(resp.cuerpo))
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c, f
}

func ok(cuerpo string) respuesta { return respuesta{http.StatusOK, cuerpo} }

var (
	usuarioOK = map[string]respuesta{
		"/internal/users/" + jugador: ok(`{"id":"` + jugador + `","nombre":"Ana","email":"ana@correo.co","rol":"player"}`),
	}
	pagosOK = map[string]respuesta{
		"/internal/wallets/" + jugador: ok(`{"user_id":"` + jugador + `","balance":1250,"cop_por_token":1000}`),
		"/internal/users/" + jugador + "/event-entries": ok(`[
			{"event_id":"` + activo + `","race_key":"2024_monza","status":"en_curso","tournament_status":"activo","starts_at":"2026-11-02T16:00:00Z","chips_final":null,"position_final":null,"prize":null},
			{"event_id":"` + cerrado + `","race_key":"2024_spa","status":"finalizado","tournament_status":"repartido","starts_at":"2026-10-20T16:00:00Z","chips_final":1500,"position_final":1,"prize":360},
			{"event_id":"` + cancel + `","race_key":"2024_imola","status":"finalizado","tournament_status":"cancelado","starts_at":"2026-10-10T16:00:00Z","chips_final":null,"position_final":null,"prize":null}]`),
		"/internal/users/" + jugador + "/bets?status=pending": ok(`[{"bet_id":"b1","market_id":"m1","event_id":"` + activo + `","option":"VER","amount":200,"mode":"individual","multiplier":2.5,"status":"pending","created_at":"2026-11-02T16:05:00Z"}]`),
		"/internal/users/" + jugador + "/ledger?limit=10":     ok(`[{"type":"purchase","tokens_delta":50,"event_id":null,"created_at":"2026-11-01T15:02:11Z"}]`),
	}
	motorOK = map[string]respuesta{
		"/internal/torneos/" + activo + "/jugadores/" + jugador: ok(`{"fichas":1340,"puesto":2,"fichas_apostadas":600}`),
	}
)

func pedir(t *testing.T, s Servicios, rol string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, s)
	req := httptest.NewRequest(http.MethodGet, "/pagina-usuario", nil)
	req.Header.Set(sesion.HeaderUsuario, jugador)
	req.Header.Set(sesion.HeaderRol, rol)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodificar(t *testing.T, rec *httptest.ResponseRecorder) Pagina {
	t.Helper()
	var p Pagina
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("cuerpo invalido: %v (%s)", err, rec.Body)
	}
	return p
}

func TestJuntaUsuariosPagosYMotorEnUnaRespuesta(t *testing.T) {
	u, _ := servicio(t, usuarioOK)
	p, fp := servicio(t, pagosOK)
	m, _ := servicio(t, motorOK)

	rec := pedir(t, Servicios{u, p, m}, "player")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	pg := decodificar(t, rec)

	if pg.Usuario.Nombre != "Ana" || pg.Saldo == nil || *pg.Saldo != 1250 || len(pg.Avisos) != 0 {
		t.Errorf("pagina = %+v", pg)
	}
	ev := *pg.Eventos
	if len(ev) != 3 {
		t.Fatalf("eventos = %+v", ev)
	}
	if *ev[0].Fichas != 1340 || *ev[0].Puesto != 2 || ev[0].Devuelta {
		t.Errorf("torneo activo: fichas y puesto del Motor, got %+v", ev[0])
	}
	if *ev[1].Fichas != 1500 || *ev[1].Puesto != 1 || *ev[1].Premio != 360 {
		t.Errorf("torneo repartido: resultado guardado en Pagos, got %+v", ev[1])
	}
	if !ev[2].Devuelta || ev[2].Fichas != nil || ev[2].Puesto != nil {
		t.Errorf("torneo cancelado: devuelta y sin fichas, got %+v", ev[2])
	}
	if a := (*pg.ApuestasActivas)[0]; a.Opcion != "VER" || a.Monto != 200 || a.Multiplicador != 2.5 || a.Modo != "individual" {
		t.Errorf("apuesta = %+v", a)
	}
	if tx := (*pg.Transacciones)[0]; tx.Tipo != "purchase" || tx.Tokens != 50 || tx.EventoID != nil {
		t.Errorf("transaccion = %+v", tx)
	}
	if len(fp.llamadas) != 4 {
		t.Errorf("Pagos recibio %v", fp.llamadas)
	}
}

func TestSiPagosNoRespondeMuestraPerfilYAvisos(t *testing.T) {
	u, _ := servicio(t, usuarioOK)
	caido := map[string]respuesta{}
	for ruta := range pagosOK {
		caido[ruta] = respuesta{http.StatusServiceUnavailable, ""}
	}
	p, _ := servicio(t, caido)
	m, fm := servicio(t, motorOK)

	rec := pedir(t, Servicios{u, p, m}, "player")
	if rec.Code != http.StatusOK {
		t.Fatalf("estado = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"saldo":null`) || !strings.Contains(rec.Body.String(), `"eventos":null`) {
		t.Errorf("las secciones de Pagos deben salir en null: %s", rec.Body)
	}
	pg := decodificar(t, rec)
	if pg.Usuario.Nombre != "Ana" || len(pg.Avisos) != 4 || pg.Avisos[0] != AvisoSaldo {
		t.Errorf("pagina = %+v", pg)
	}
	if len(fm.llamadas) != 0 {
		t.Errorf("sin eventos no hay nada que pedirle al Motor: %v", fm.llamadas)
	}
}

func TestSiElMotorFallaLasFichasSalenEnNullConAviso(t *testing.T) {
	u, _ := servicio(t, usuarioOK)
	p, _ := servicio(t, pagosOK)
	m, _ := servicio(t, map[string]respuesta{}) // 404: ruta aun no implementada

	pg := decodificar(t, pedir(t, Servicios{u, p, m}, "player"))
	if ev := (*pg.Eventos)[0]; ev.Fichas != nil || ev.Puesto != nil {
		t.Errorf("fichas deben ser null: %+v", ev)
	}
	if len(pg.Avisos) != 1 || pg.Avisos[0] != AvisoFichas {
		t.Errorf("avisos = %v", pg.Avisos)
	}
	if *pg.Saldo != 1250 {
		t.Errorf("el resto de la pagina debe cargar")
	}
}

func TestSinUsuariosNoHayPagina(t *testing.T) {
	p, _ := servicio(t, pagosOK)
	m, _ := servicio(t, motorOK)

	caido, _ := servicio(t, map[string]respuesta{"/internal/users/" + jugador: {http.StatusServiceUnavailable, ""}})
	if rec := pedir(t, Servicios{caido, p, m}, "player"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Usuarios caido: estado = %d", rec.Code)
	}
	borrado, _ := servicio(t, map[string]respuesta{})
	if rec := pedir(t, Servicios{borrado, p, m}, "player"); rec.Code != http.StatusUnauthorized {
		t.Errorf("usuario inexistente: estado = %d", rec.Code)
	}
}

func TestSoloJugadores(t *testing.T) {
	u, fu := servicio(t, usuarioOK)
	p, _ := servicio(t, pagosOK)
	m, _ := servicio(t, motorOK)
	if rec := pedir(t, Servicios{u, p, m}, "admin"); rec.Code != http.StatusForbidden {
		t.Errorf("admin: estado = %d", rec.Code)
	}
	if len(fu.llamadas) != 0 {
		t.Errorf("no debia llamar a nadie")
	}
}
