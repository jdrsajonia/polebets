package dinamicas

import (
	"bytes"
	"context"
	"errors"
	"log"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fuenteFalsa struct {
	sim             Simulacion
	hay             bool
	errCarrera      error
	llamadasCarrera int
	eventos         []Evento
	lecturas        [][2]int64
}

func (f *fuenteFalsa) SimulacionActual(context.Context) (Simulacion, bool, error) {
	return f.sim, f.hay, nil
}

func (f *fuenteFalsa) Carrera(context.Context, string) (DatosCarrera, error) {
	f.llamadasCarrera++
	if f.errCarrera != nil {
		return DatosCarrera{}, f.errCarrera
	}
	return DatosCarrera{TotalVueltas: 50, Pilotos: codigos}, nil
}

func (f *fuenteFalsa) Eventos(_ context.Context, _ string, desde, hasta int64) ([]Evento, error) {
	f.lecturas = append(f.lecturas, [2]int64{desde, hasta})
	var res []Evento
	for _, ev := range f.eventos {
		if ev.T > desde && ev.T <= hasta {
			res = append(res, ev)
		}
	}
	return res, nil
}

// pagosFalsoW anota las llamadas y devuelve, por operacion, los errores de
// su cola (uno por llamada) antes de responder bien.
type pagosFalsoW struct {
	llamadas []string
	altas    []Alta
	errores  map[string][]error
}

func (p *pagosFalsoW) siguiente(op string) error {
	if len(p.errores[op]) == 0 {
		return nil
	}
	err := p.errores[op][0]
	p.errores[op] = p.errores[op][1:]
	return err
}

func (p *pagosFalsoW) Crear(_ context.Context, a Alta) (bool, error) {
	p.llamadas = append(p.llamadas, "crear "+a.Mercado.Plantilla+" "+a.Estado)
	if err := p.siguiente("crear"); err != nil {
		return false, err
	}
	p.altas = append(p.altas, a)
	return true, nil
}

func (p *pagosFalsoW) Abrir(context.Context, string) error { return errors.New("no se usa") }

func (p *pagosFalsoW) Cerrar(_ context.Context, id string) error {
	p.llamadas = append(p.llamadas, "cerrar")
	return p.siguiente("cerrar")
}

func (p *pagosFalsoW) Resolver(_ context.Context, id, opcion string) error {
	p.llamadas = append(p.llamadas, "resolver "+opcion)
	return p.siguiente("resolver")
}

type difusorFalso struct{ msgs []MensajeDinamico }

func (d *difusorFalso) Difundir(tipo string, payload any) error {
	if tipo != TipoMercadoDinamico {
		return errors.New("tipo inesperado " + tipo)
	}
	d.msgs = append(d.msgs, payload.(MensajeDinamico))
	return nil
}

func (d *difusorFalso) estados() []string {
	var r []string
	for _, m := range d.msgs {
		s := m.Status
		if m.WinningOption != nil {
			s += " " + *m.WinningOption
		}
		r = append(r, s)
	}
	return r
}

var iniciadaPrueba = time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)

// escenarioWorker: cambio_lider abre en 100 s, cierra en 130 s y se resuelve
// en 190 s con "No" (nadie pasa a VER).
func escenarioWorker() (*Worker, *fuenteFalsa, *pagosFalsoW, *difusorFalso) {
	f := &fuenteFalsa{
		hay:     true,
		sim:     Simulacion{ID: "sim-a", CarreraID: "9165", Estado: SimEnCurso, TCarreraMs: math.MinInt64, IniciadaEn: iniciadaPrueba},
		eventos: con(enVuelta2(), intv(100000, 4, f(0.5))),
	}
	p := &pagosFalsoW{errores: map[string][]error{}}
	d := &difusorFalso{}
	return NuevoWorker(f, p, d, 30*time.Second, 60*time.Second), f, p, d
}

// ticks pone el reloj en cada valor y procesa.
func ticks(t *testing.T, w *Worker, f *fuenteFalsa, relojes ...int64) {
	t.Helper()
	for _, r := range relojes {
		f.sim.TCarreraMs = r
		_ = w.Procesar(context.Background())
	}
}

func TestWorkerEnVivo(t *testing.T) {
	w, f, p, d := escenarioWorker()
	ticks(t, w, f, 95000, 100000, 120000, 130000, 189000, 190000, 200000)
	want := []string{"crear cambio_lider abierto", "cerrar", "resolver No"}
	if !reflect.DeepEqual(p.llamadas, want) {
		t.Fatalf("llamadas = %v", p.llamadas)
	}
	if got := d.estados(); !reflect.DeepEqual(got, []string{"abierto", "cerrado", "cerrado No"}) {
		t.Fatalf("difusion = %v", got)
	}
	a := p.altas[0]
	if !a.AbreEn.Equal(iniciadaPrueba.Add(100*time.Second)) || !a.CierraEn.Equal(iniciadaPrueba.Add(130*time.Second)) {
		t.Fatalf("opens_at = %v, closes_at = %v", a.AbreEn, a.CierraEn)
	}
	m := d.msgs[0]
	if m.MarketID != a.Mercado.ID || m.EventID != "sim-a" || m.Template != PlantillaCambioLider || m.RaceCloseS != 130 ||
		len(m.Options) != 2 || m.Options[0].Opcion != OpcionSi {
		t.Fatalf("mensaje = %+v", m)
	}
	// Lecturas contiguas: (cursor, reloj] sin huecos ni solapes.
	for i := 1; i < len(f.lecturas); i++ {
		if f.lecturas[i][0] != f.lecturas[i-1][1] {
			t.Fatalf("lecturas = %v", f.lecturas)
		}
	}
}

func TestWorkerEsperaElLatido(t *testing.T) {
	w, f, p, _ := escenarioWorker()
	_ = w.Procesar(context.Background()) // t_carrera_ms ausente
	if f.llamadasCarrera != 0 || len(f.lecturas) != 0 || len(p.llamadas) != 0 {
		t.Fatalf("no debia hacer nada: carrera=%d lecturas=%v", f.llamadasCarrera, f.lecturas)
	}
}

func TestWorkerReintentaLaCarrera(t *testing.T) {
	w, f, _, _ := escenarioWorker()
	f.errCarrera = errors.New("mongo caido")
	f.sim.TCarreraMs = 50000
	if err := w.Procesar(context.Background()); err == nil || w.gestor != nil || len(f.lecturas) != 0 {
		t.Fatalf("err = %v, gestor = %v", err, w.gestor)
	}
	f.errCarrera = nil
	if err := w.Procesar(context.Background()); err != nil || w.gestor == nil || f.llamadasCarrera != 2 {
		t.Fatalf("err = %v, llamadas = %d", err, f.llamadasCarrera)
	}
	_ = w.Procesar(context.Background())
	if f.llamadasCarrera != 2 {
		t.Fatalf("la carrera se lee una vez por simulacion: %d", f.llamadasCarrera)
	}
}

func TestWorkerReintentableNoAvanzaNiDuplica(t *testing.T) {
	w, f, p, d := escenarioWorker()
	p.errores["crear"] = []error{ErrReintentable, ErrReintentable}
	ticks(t, w, f, 95000, 100000)
	lecturas := len(f.lecturas)
	ticks(t, w, f, 100500) // vuelve a fallar: no lee eventos nuevos
	if len(f.lecturas) != lecturas {
		t.Fatalf("con cola pendiente no debe leer: %v", f.lecturas)
	}
	ticks(t, w, f, 101000, 130000, 190000)
	want := []string{"crear cambio_lider abierto", "crear cambio_lider abierto", "crear cambio_lider abierto", "cerrar", "resolver No"}
	if !reflect.DeepEqual(p.llamadas, want) {
		t.Fatalf("llamadas = %v", p.llamadas)
	}
	if len(p.altas) != 1 || len(d.msgs) != 3 {
		t.Fatalf("altas = %d, mensajes = %d", len(p.altas), len(d.msgs))
	}
}

func TestWorkerConflictoNoReintentaEnBucle(t *testing.T) {
	w, f, p, _ := escenarioWorker()
	p.errores["crear"] = []error{ErrConflicto}
	ticks(t, w, f, 95000, 100000, 100500, 101000, 130000)
	want := []string{"crear cambio_lider abierto", "cerrar"}
	if !reflect.DeepEqual(p.llamadas, want) {
		t.Fatalf("llamadas = %v", p.llamadas)
	}
}

func TestWorkerRecuperacion(t *testing.T) {
	casos := []struct {
		nombre    string
		reloj     int64
		llamadas  []string
		difundido []string
	}{
		// El Motor arranca con el mercado ya cerrado: se crea programado y se
		// cierra; nada se difunde.
		{"race_close ya paso", 200000, []string{"crear cambio_lider programado", "cerrar", "resolver No"}, nil},
		// Arranca en plena ventana pero 10 s tarde: queda abierto (aun cabe
		// apostar en Pagos) y no se anuncia.
		{"en plena ventana", 110000, []string{"crear cambio_lider abierto"}, nil},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			w, f, p, d := escenarioWorker()
			ticks(t, w, f, c.reloj)
			if !reflect.DeepEqual(p.llamadas, c.llamadas) || !reflect.DeepEqual(d.estados(), c.difundido) {
				t.Fatalf("llamadas = %v, difusion = %v", p.llamadas, d.estados())
			}
		})
	}
	t.Run("lo que sigue despues de recuperar si se difunde", func(t *testing.T) {
		w, f, _, d := escenarioWorker()
		ticks(t, w, f, 110000, 130000)
		if got := d.estados(); !reflect.DeepEqual(got, []string{"cerrado"}) {
			t.Fatalf("difusion = %v", got)
		}
	})
}

func TestWorkerSimulacionTerminada(t *testing.T) {
	t.Run("se procesa una vez y no se difunde", func(t *testing.T) {
		w, f, p, d := escenarioWorker()
		f.sim.Estado = SimFinalizada
		ticks(t, w, f, 200000, 200000, 200000)
		want := []string{"crear cambio_lider programado", "cerrar", "resolver No"}
		if !reflect.DeepEqual(p.llamadas, want) || len(d.msgs) != 0 {
			t.Fatalf("llamadas = %v, difusion = %v", p.llamadas, d.estados())
		}
	})
	t.Run("si una accion falla se abandona", func(t *testing.T) {
		w, f, p, _ := escenarioWorker()
		f.sim.Estado = SimFinalizada
		p.errores["cerrar"] = []error{ErrReintentable}
		ticks(t, w, f, 200000, 200000, 200000)
		want := []string{"crear cambio_lider programado", "cerrar"}
		if !reflect.DeepEqual(p.llamadas, want) {
			t.Fatalf("llamadas = %v", p.llamadas)
		}
	})
	t.Run("fallida en plena ventana: cierra y no resuelve", func(t *testing.T) {
		w, f, p, d := escenarioWorker()
		ticks(t, w, f, 95000, 100000, 110000)
		f.sim.Estado = SimFallida
		ticks(t, w, f, 110000, 110000)
		want := []string{"crear cambio_lider abierto", "cerrar"}
		if !reflect.DeepEqual(p.llamadas, want) || !reflect.DeepEqual(d.estados(), []string{"abierto", "cerrado"}) {
			t.Fatalf("llamadas = %v, difusion = %v", p.llamadas, d.estados())
		}
	})
	t.Run("finalizada antes de race_close+O queda sin resolver", func(t *testing.T) {
		w, f, p, _ := escenarioWorker()
		ticks(t, w, f, 95000, 100000, 150000)
		f.sim.Estado = SimFinalizada
		ticks(t, w, f, 150000)
		if want := []string{"crear cambio_lider abierto", "cerrar"}; !reflect.DeepEqual(p.llamadas, want) {
			t.Fatalf("llamadas = %v", p.llamadas)
		}
	})
}

func TestWorkerCambioDeSimulacion(t *testing.T) {
	w, f, p, _ := escenarioWorker()
	ticks(t, w, f, 95000, 100000, 110000) // mercado de sim-a abierto
	f.sim = Simulacion{ID: "sim-b", CarreraID: "9165", Estado: SimEnCurso, TCarreraMs: math.MinInt64, IniciadaEn: iniciadaPrueba}
	_ = w.Procesar(context.Background())
	if want := []string{"crear cambio_lider abierto", "cerrar"}; !reflect.DeepEqual(p.llamadas, want) {
		t.Fatalf("llamadas = %v", p.llamadas)
	}
	if w.gestor != nil || w.sim.ID != "sim-b" {
		t.Fatalf("debe esperar el latido de sim-b: gestor = %v", w.gestor)
	}
	// sim-b arranca de cero con su propio Gestor y sus propios market_id.
	ticks(t, w, f, 95000, 100000)
	if len(p.altas) != 2 || p.altas[1].Mercado.EventoID != "sim-b" || p.altas[1].Mercado.ID == p.altas[0].Mercado.ID {
		t.Fatalf("altas = %+v", p.altas)
	}
}

func TestWorkerAvisoDeReintentoCada10s(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	w, f, p, _ := escenarioWorker()
	ahora := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	w.Ahora = func() time.Time { return ahora }
	tick := func(reloj int64) {
		f.sim.TCarreraMs = reloj
		_ = w.Procesar(context.Background())
		ahora = ahora.Add(500 * time.Millisecond)
	}
	avisos := func() int { return strings.Count(buf.String(), "queda para el siguiente tick") }

	// El evento aun no existe en Pagos: 25 ticks seguidos (12,5 s) fallando.
	for i := 0; i < 25; i++ {
		p.errores["crear"] = append(p.errores["crear"], ErrReintentable)
	}
	tick(95000)
	for i := 0; i < 25; i++ {
		tick(100000)
	}
	if n := avisos(); n != 2 { // en 0 s y en 10 s
		t.Fatalf("avisos = %d:\n%s", n, buf.String())
	}
	tick(100000) // ahora si se crea
	if len(p.altas) != 1 {
		t.Fatalf("altas = %d", len(p.altas))
	}

	// Un fallo nuevo del mismo mercado despues de un exito avisa enseguida.
	p.errores["cerrar"] = []error{ErrReintentable}
	tick(130000)
	if n := avisos(); n != 3 {
		t.Fatalf("avisos = %d:\n%s", n, buf.String())
	}
}
