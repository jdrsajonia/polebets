package posiciones

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arquisoft/motor/internal/auth"
	"github.com/arquisoft/motor/internal/ingest"
	"github.com/arquisoft/motor/internal/ws"
)

var instante = time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)

func pilotosPrueba() []ingest.PilotoCarrera {
	return []ingest.PilotoCarrera{
		{Numero: 1, Codigo: "VER", Nombre: "Max VERSTAPPEN", Equipo: "Red Bull Racing", Color: "3671C6"},
		{Numero: 4, Codigo: "NOR", Nombre: "Lando NORRIS", Equipo: "McLaren", Color: "FF8000"},
		{Numero: 16, Codigo: "LEC", Nombre: "Charles LECLERC", Equipo: "Ferrari", Color: ""},
	}
}

func tableroPrueba() *Tablero {
	t := NuevoTablero()
	t.ahora = func() time.Time { return instante }
	return t
}

// numeros devuelve los numeros de carro en el orden de la foto.
func numeros(f FotoPosiciones) []int {
	r := make([]int, len(f.Pilotos))
	for i, p := range f.Pilotos {
		r[i] = p.Numero
	}
	return r
}

func igual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Tablero ---

func TestSinCarreraNoHayFoto(t *testing.T) {
	tab := tableroPrueba()
	if _, ok := tab.Foto(); ok {
		t.Fatal("sin Iniciar no debe haber foto")
	}
	if _, ok := tab.Actualizar(1, 1); ok {
		t.Fatal("Actualizar sin carrera debe ignorarse")
	}
}

func TestIniciarArmaLaFotoOrdenada(t *testing.T) {
	tab := tableroPrueba()
	foto := tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 3, 4: 1, 16: 2})
	if foto.CarreraID != "2024_monza" || !foto.ActualizadoEn.Equal(instante) || !foto.EmitidoEn.IsZero() {
		t.Fatalf("cabecera de la foto: %+v", foto)
	}
	if got := numeros(foto); !igual(got, []int{4, 16, 1}) {
		t.Fatalf("orden = %v, quiero [4 16 1]", got)
	}
	if guardada, ok := tab.Foto(); !ok || !igual(numeros(guardada), numeros(foto)) {
		t.Fatalf("Foto() = %+v, %v", guardada, ok)
	}
}

func TestActualizarMueveAlPilotoYDevuelveLaFotoCompleta(t *testing.T) {
	tab := tableroPrueba()
	tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1, 4: 2, 16: 3})

	// Adelantamiento de NOR sobre VER: OpenF1 lo manda en dos eventos.
	foto, ok := tab.Actualizar(4, 1)
	if !ok {
		t.Fatal("Actualizar con carrera debe aplicar")
	}
	// Entre los dos eventos hay dos P1: salen los dos, sin romper nada.
	if got := numeros(foto); !igual(got, []int{1, 4, 16}) || foto.Pilotos[0].Posicion != 1 || foto.Pilotos[1].Posicion != 1 {
		t.Fatalf("foto intermedia = %+v", foto.Pilotos)
	}
	foto, _ = tab.Actualizar(1, 2)
	if got := numeros(foto); !igual(got, []int{4, 1, 16}) || len(foto.Pilotos) != 3 {
		t.Fatalf("orden = %v, quiero [4 1 16] con los 3 pilotos", got)
	}
}

func TestIniciarReemplazaLaCarreraAnterior(t *testing.T) {
	tab := tableroPrueba()
	posiciones := map[int]int{1: 1, 4: 2, 16: 3}
	tab.Iniciar("2024_monza", pilotosPrueba(), posiciones)
	posiciones[99] = 4 // el tablero copio el mapa: esto no lo afecta

	foto := tab.Iniciar("2024_spa", []ingest.PilotoCarrera{{Numero: 44, Codigo: "HAM"}}, map[int]int{44: 1})
	if foto.CarreraID != "2024_spa" || !igual(numeros(foto), []int{44}) {
		t.Fatalf("la carrera nueva no reemplazo a la anterior: %+v", foto)
	}
}

func TestIniciarSinPosicionesDaFotoVacia(t *testing.T) {
	tab := tableroPrueba()
	foto := tab.Iniciar("2024_monza", pilotosPrueba(), nil)
	if foto.Pilotos == nil || len(foto.Pilotos) != 0 {
		t.Fatalf("pilotos = %#v, quiero [] (no null)", foto.Pilotos)
	}
	cuerpo, _ := json.Marshal(foto)
	if !strings.Contains(string(cuerpo), `"pilotos":[]`) {
		t.Fatalf("JSON = %s", cuerpo)
	}
	if _, ok := tab.Foto(); !ok {
		t.Fatal("con carrera iniciada Foto() debe dar true aunque no haya pilotos")
	}
}

// Lo escribe la reproduccion y lo lee GET /posiciones a la vez (go test -race).
func TestTableroConcurrente(t *testing.T) {
	tab := tableroPrueba()
	tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1, 4: 2, 16: 3})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				tab.Actualizar([]int{1, 4, 16}[j%3], j%3+1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if foto, ok := tab.Foto(); ok {
					_, _ = json.Marshal(foto)
				}
			}
		}()
	}
	wg.Wait()
}

// --- Armado de cada piloto ---

func TestArmarPiloto(t *testing.T) {
	tab := tableroPrueba()
	foto := tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1, 4: 2, 16: 3, 43: 4})
	porNumero := map[int]Piloto{}
	for _, p := range foto.Pilotos {
		porNumero[p.Numero] = p
	}

	ver := porNumero[1]
	if ver != (Piloto{Posicion: 1, Numero: 1, Codigo: "VER", Nombre: "Max VERSTAPPEN", Equipo: "Red Bull Racing",
		ColorEquipo: "#3671C6", Diferencia: DiferenciaLider}) {
		t.Errorf("P1 = %+v", ver)
	}
	if nor := porNumero[4]; nor.ColorEquipo != "#FF8000" || nor.Diferencia != SinDiferencia {
		t.Errorf("P2 = %+v", nor)
	}
	// Sin color en la lista: se omite en el JSON.
	if lec := porNumero[16]; lec.ColorEquipo != "" {
		t.Errorf("LEC sin color = %+v", lec)
	}
	cuerpo, _ := json.Marshal(porNumero[16])
	if strings.Contains(string(cuerpo), "color_equipo") {
		t.Errorf("color vacio debe omitirse: %s", cuerpo)
	}
	// Piloto que no esta en lista_pilotos: codigo = numero, sin nombre.
	if col := porNumero[43]; col.Codigo != "43" || col.Nombre != "" || col.Equipo != "" || col.ColorEquipo != "" {
		t.Errorf("piloto sin datos = %+v", col)
	}
}

// --- Difusor ---

type hubFalso struct {
	mu       sync.Mutex
	mensajes []ws.Mensaje
	fallar   error
}

func (h *hubFalso) Difundir(m ws.Mensaje) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mensajes = append(h.mensajes, m)
	return h.fallar
}

func TestDifusorDifundeLaFotoCompleta(t *testing.T) {
	hub := &hubFalso{}
	d := NuevoDifusor(tableroPrueba(), hub)
	envio := instante.Add(time.Second)
	d.ahora = func() time.Time { return envio }

	d.Actualizar(1, 1) // sin carrera: no se difunde nada
	d.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1, 4: 2})
	d.Actualizar(4, 1)

	if len(hub.mensajes) != 2 {
		t.Fatalf("mensajes = %d, quiero 2 (foto inicial + cambio)", len(hub.mensajes))
	}
	for i, m := range hub.mensajes {
		foto, ok := m.Payload.(FotoPosiciones)
		if m.Type != ws.TipoPosiciones || !ok || !foto.EmitidoEn.Equal(envio) || len(foto.Pilotos) != 2 {
			t.Fatalf("mensaje %d = %+v", i, m)
		}
	}
	// El tablero guarda la foto sin emitido_en: eso es de cada envio.
	if foto, _ := d.tablero.Foto(); !foto.EmitidoEn.IsZero() {
		t.Fatalf("el tablero no debe guardar emitido_en: %+v", foto)
	}
}

func TestDifusorSigueSiFallaElEnvio(t *testing.T) {
	hub := &hubFalso{fallar: ws.ErrHubDetenido}
	d := NuevoDifusor(tableroPrueba(), hub)
	d.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1})
	d.Actualizar(4, 2)
	if foto, _ := d.tablero.Foto(); len(foto.Pilotos) != 2 {
		t.Fatalf("el tablero debe seguir actualizandose: %+v", foto)
	}
}

// --- GET /posiciones ---

// validadorFalso acepta solo el token "ok".
type validadorFalso struct{}

func (validadorFalso) Validar(token string) (auth.Sesion, error) {
	if token != "ok" {
		return auth.Sesion{}, errors.New("token invalido")
	}
	return auth.Sesion{UserID: "00000000-0000-0000-0000-000000000002", Rol: auth.RolJugador}, nil
}

func pedir(tab *Tablero, conCookie bool) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	Register(mux, tab, auth.NuevoAutenticador(validadorFalso{}).ExigirSesion)
	r := httptest.NewRequest(http.MethodGet, "/posiciones", nil)
	if conCookie {
		r.AddCookie(&http.Cookie{Name: auth.NombreCookie, Value: "ok"})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestGetPosiciones(t *testing.T) {
	t.Run("sin sesion: 401", func(t *testing.T) {
		tab := tableroPrueba()
		tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 1})
		if w := pedir(tab, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("sin carrera: 404", func(t *testing.T) {
		w := pedir(tableroPrueba(), true)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no hay una carrera en curso") {
			t.Fatalf("status=%d cuerpo=%s", w.Code, w.Body.String())
		}
	})
	t.Run("con carrera: 200 con la foto", func(t *testing.T) {
		tab := tableroPrueba()
		tab.Iniciar("2024_monza", pilotosPrueba(), map[int]int{1: 2, 4: 1})
		w := pedir(tab, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d cuerpo=%s", w.Code, w.Body.String())
		}
		var foto struct {
			CarreraID     string `json:"carrera_id"`
			ActualizadoEn string `json:"actualizado_en"`
			Pilotos       []struct {
				Posicion    int    `json:"posicion"`
				Codigo      string `json:"codigo"`
				ColorEquipo string `json:"color_equipo"`
				Diferencia  string `json:"diferencia"`
			} `json:"pilotos"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &foto); err != nil {
			t.Fatal(err)
		}
		if foto.CarreraID != "2024_monza" || foto.ActualizadoEn != "2026-10-12T15:00:00Z" || len(foto.Pilotos) != 2 ||
			foto.Pilotos[0].Codigo != "NOR" || foto.Pilotos[0].Diferencia != "Líder" || foto.Pilotos[1].ColorEquipo != "#3671C6" {
			t.Fatalf("foto = %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "emitido_en") || strings.Contains(w.Body.String(), "vuelta") {
			t.Fatalf("GET no lleva emitido_en ni vuelta: %s", w.Body.String())
		}
	})
}
