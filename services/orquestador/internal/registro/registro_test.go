package registro

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arquisoft/orquestador/internal/interno"
)

const token = "token-interno-de-pruebas-orquestador"

// servicioFalso responde con los estados de la lista, uno por llamada (el
// ultimo se repite), y guarda lo que recibio en cada llamada.
type servicioFalso struct {
	mu       sync.Mutex
	estados  []int
	cuerpo   string
	llamadas []llamada
}

type llamada struct {
	ruta, clave, token string
	cuerpo             map[string]string
}

func nuevoServicio(t *testing.T, cuerpo string, estados ...int) (*interno.Cliente, *servicioFalso) {
	t.Helper()
	f := &servicioFalso{estados: estados, cuerpo: cuerpo}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		var c map[string]string
		_ = json.Unmarshal(b, &c)
		f.llamadas = append(f.llamadas, llamada{
			ruta:   r.Method + " " + r.URL.Path,
			clave:  r.Header.Get("Idempotency-Key"),
			token:  r.Header.Get("X-Internal-Service-Token"),
			cuerpo: c,
		})
		w.WriteHeader(f.estados[min(len(f.llamadas), len(f.estados))-1])
		_, _ = w.Write([]byte(f.cuerpo))
	}))
	t.Cleanup(srv.Close)
	c := interno.Nuevo(srv.URL, token)
	c.Esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return c, f
}

func (f *servicioFalso) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.llamadas)
}

const usuarioUsuarios = `{"id":"ID","nombre":"Ana","email":"ana@correo.co","rol":"player"}`

func registrar(t *testing.T, pagos, usuarios Servicio, cuerpo string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, pagos, usuarios)
	req := httptest.NewRequest(http.MethodPost, "/registro", strings.NewReader(cuerpo))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const valido = `{"nombre":" Ana ","email":"ana@correo.co","contrasena":"clave-segura"}`

func codigo(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var c struct{ Codigo string }
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	return c.Codigo
}

func TestRegistroCreaBilleteraYLuegoCuentaConElMismoID(t *testing.T) {
	pagos, fp := nuevoServicio(t, `{"user_id":"x","balance":0,"cop_por_token":1000}`, http.StatusCreated)
	usuarios, fu := nuevoServicio(t, usuarioUsuarios, http.StatusCreated)

	rec := registrar(t, pagos, usuarios, valido)

	if rec.Code != http.StatusCreated {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	var r Respuesta
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || r.Usuario.Rol != "player" {
		t.Fatalf("respuesta inesperada: %s", rec.Body)
	}

	pb, us := fp.llamadas[0], fu.llamadas[0]
	id := pb.cuerpo["user_id"]
	if pb.ruta != "POST /internal/wallets" || pb.clave != "wallet:"+id || pb.token != token {
		t.Errorf("llamada a Pagos = %+v", pb)
	}
	if us.ruta != "POST /internal/users" || us.cuerpo["id"] != id || us.clave != "user:"+id {
		t.Errorf("llamada a Usuarios = %+v (id de la billetera %s)", us, id)
	}
	if us.cuerpo["nombre"] != "Ana" || us.cuerpo["email"] != "ana@correo.co" || us.cuerpo["contrasena"] != "clave-segura" {
		t.Errorf("cuerpo a Usuarios = %v", us.cuerpo)
	}
}

func TestBilleteraQueYaExistiaTambienSirve(t *testing.T) {
	pagos, _ := nuevoServicio(t, `{}`, http.StatusOK)
	usuarios, _ := nuevoServicio(t, usuarioUsuarios, http.StatusOK)
	if rec := registrar(t, pagos, usuarios, valido); rec.Code != http.StatusCreated {
		t.Fatalf("estado = %d", rec.Code)
	}
}

func TestSiPagosNoRespondeNoSeCreaLaCuenta(t *testing.T) {
	pagos, fp := nuevoServicio(t, `{}`, http.StatusServiceUnavailable)
	usuarios, fu := nuevoServicio(t, usuarioUsuarios, http.StatusCreated)

	rec := registrar(t, pagos, usuarios, valido)

	if rec.Code != http.StatusServiceUnavailable || codigo(t, rec) != "servicio_no_disponible" {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	if fp.n() != 3 {
		t.Errorf("Pagos recibio %d intentos, se esperaban 3 (es idempotente)", fp.n())
	}
	if fu.n() != 0 {
		t.Errorf("Usuarios no debia recibir nada y recibio %d llamadas", fu.n())
	}
}

func TestReintentaUsuariosConLaMismaClave(t *testing.T) {
	pagos, _ := nuevoServicio(t, `{}`, http.StatusCreated)
	usuarios, fu := nuevoServicio(t, usuarioUsuarios, http.StatusBadGateway, http.StatusOK)

	if rec := registrar(t, pagos, usuarios, valido); rec.Code != http.StatusCreated {
		t.Fatalf("estado = %d, cuerpo %s", rec.Code, rec.Body)
	}
	if fu.n() != 2 || fu.llamadas[0].cuerpo["id"] != fu.llamadas[1].cuerpo["id"] {
		t.Errorf("el reintento debe usar el mismo id: %+v", fu.llamadas)
	}
}

func TestCorreoRegistradoYDatosInvalidosDeUsuarios(t *testing.T) {
	casos := map[int]struct {
		estado int
		codigo string
	}{
		http.StatusConflict:            {http.StatusConflict, "correo_registrado"},
		http.StatusUnprocessableEntity: {http.StatusBadRequest, "cuerpo_invalido"},
		http.StatusUnauthorized:        {http.StatusInternalServerError, "error_interno"},
	}
	for deUsuarios, esperado := range casos {
		pagos, _ := nuevoServicio(t, `{}`, http.StatusCreated)
		usuarios, fu := nuevoServicio(t, `{"detail":"x"}`, deUsuarios)
		rec := registrar(t, pagos, usuarios, valido)
		if rec.Code != esperado.estado || codigo(t, rec) != esperado.codigo {
			t.Errorf("Usuarios %d -> %d %s; se esperaba %d %s", deUsuarios, rec.Code, codigo(t, rec), esperado.estado, esperado.codigo)
		}
		if fu.n() != 1 {
			t.Errorf("Usuarios %d: un 4xx no se reintenta, hubo %d llamadas", deUsuarios, fu.n())
		}
	}
}

func TestCuerpoInvalidoNoLlamaANadie(t *testing.T) {
	cuerpos := []string{
		``,
		`no-json`,
		`{"nombre":"Ana","email":"ana@correo.co"}`,
		`{"nombre":"  ","email":"ana@correo.co","contrasena":"x"}`,
		`{"nombre":"Ana","email":"ana@correo.co","contrasena":"x","rol":"admin"}`,
		`{"nombre":1,"email":"ana@correo.co","contrasena":"x"}`,
	}
	for _, c := range cuerpos {
		pagos, fp := nuevoServicio(t, `{}`, http.StatusCreated)
		usuarios, fu := nuevoServicio(t, usuarioUsuarios, http.StatusCreated)
		rec := registrar(t, pagos, usuarios, c)
		if rec.Code != http.StatusBadRequest || codigo(t, rec) != "cuerpo_invalido" {
			t.Errorf("%q -> %d %s", c, rec.Code, rec.Body)
		}
		if fp.n()+fu.n() != 0 {
			t.Errorf("%q: no debia llamar a ningun servicio", c)
		}
	}
}

func TestNuevoIDEsUUIDv4(t *testing.T) {
	patron := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	vistos := map[string]bool{}
	for range 100 {
		id := NuevoID()
		if !patron.MatchString(id) || vistos[id] {
			t.Fatalf("id invalido o repetido: %s", id)
		}
		vistos[id] = true
	}
}
