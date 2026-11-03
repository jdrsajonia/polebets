package visor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// fuenteFalsa imita lo que el Ingestor deja en Mongo.
type fuenteFalsa struct {
	sim          Simulacion
	haySim       bool
	trazado      Trazado
	errTrazado   error
	lecturasTraz int
	muestras     []Muestra // de todos los autos, ordenadas por t
	fallar       bool
	consultas    [][2]int64
}

func (f *fuenteFalsa) SimulacionVigente(context.Context) (Simulacion, bool, error) {
	return f.sim, f.haySim, nil
}

func (f *fuenteFalsa) Trazado(context.Context, string) (Trazado, error) {
	f.lecturasTraz++
	return f.trazado, f.errTrazado
}

func (f *fuenteFalsa) Muestras(_ context.Context, _ string, desde, hasta int64) ([]Muestra, error) {
	f.consultas = append(f.consultas, [2]int64{desde, hasta})
	if f.fallar {
		return nil, errors.New("mongo caido")
	}
	var r []Muestra
	for _, m := range f.muestras {
		if m.T > desde && m.T <= hasta {
			r = append(r, m)
		}
	}
	return r, nil
}

// difusorFalso guarda lo que se difundio.
type difusorFalso struct {
	tipos    []string
	payloads []any
}

func (d *difusorFalso) Difundir(tipo string, payload any) error {
	d.tipos = append(d.tipos, tipo)
	d.payloads = append(d.payloads, payload)
	return nil
}

// cuenta cuantos mensajes de un tipo se difundieron.
func (d *difusorFalso) cuenta(tipo string) int {
	n := 0
	for _, t := range d.tipos {
		if t == tipo {
			n++
		}
	}
	return n
}

// ultimas devuelve el payload del ultimo mensaje "coordenadas".
func (d *difusorFalso) ultimas(t *testing.T) MensajeCoordenadas {
	t.Helper()
	for i := len(d.tipos) - 1; i >= 0; i-- {
		if d.tipos[i] == TipoCoordenadas {
			return d.payloads[i].(MensajeCoordenadas)
		}
	}
	t.Fatal("no se difundio ningun mensaje de coordenadas")
	return MensajeCoordenadas{}
}

// relojFalso permite avanzar la hora real a mano.
type relojFalso struct{ t time.Time }

func (r *relojFalso) ahora() time.Time { return r.t }

func trazadoPrueba() Trazado {
	return Trazado{Piloto: 44, Vuelta: 47, Puntos: []Punto{{0, 0}, {100, 0}, {100, 100}, {0, 100}}}
}

func armar(f *fuenteFalsa) (*Lector, *difusorFalso, *relojFalso) {
	d := &difusorFalso{}
	r := &relojFalso{t: time.Date(2026, 11, 4, 15, 0, 0, 0, time.UTC)}
	l := NuevoLector(f, d, time.Second)
	l.Ahora = r.ahora
	return l, d, r
}

func paso(t *testing.T, l *Lector) {
	t.Helper()
	if err := l.Paso(context.Background()); err != nil {
		t.Fatalf("Paso: %v", err)
	}
}

func TestProgramadaDifundeLaPistaPeroNoCoordenadas(t *testing.T) {
	f := &fuenteFalsa{haySim: true, sim: Simulacion{ID: "s1", CarreraID: "9165", Estado: "programada", TCarreraMs: math.MinInt64}, trazado: trazadoPrueba()}
	l, d, _ := armar(f)
	paso(t, l)
	if d.cuenta(TipoPista) != 1 || d.cuenta(TipoCoordenadas) != 0 {
		t.Fatalf("difundido %v; se esperaba solo la pista", d.tipos)
	}
	p := d.payloads[0].(MensajePista)
	if p.Piloto != 44 || p.Vuelta != 47 || len(p.Puntos) != 4 || p.Estado != "programada" {
		t.Fatalf("pista inesperada: %+v", p)
	}
	if len(f.consultas) != 0 {
		t.Fatalf("antes de la largada no se leen coordenadas: %v", f.consultas)
	}
}

func TestLaPistaSeReenviaCada5sYElTrazadoSeLeeUnaVez(t *testing.T) {
	f := &fuenteFalsa{haySim: true, sim: Simulacion{ID: "s1", CarreraID: "9165", Estado: "programada", TCarreraMs: math.MinInt64}, trazado: trazadoPrueba()}
	l, d, r := armar(f)
	paso(t, l)
	r.t = r.t.Add(4 * time.Second)
	paso(t, l)
	if d.cuenta(TipoPista) != 1 {
		t.Fatalf("antes de 5 s no se reenvia: %d pistas", d.cuenta(TipoPista))
	}
	r.t = r.t.Add(time.Second)
	paso(t, l)
	if d.cuenta(TipoPista) != 2 {
		t.Fatalf("a los 5 s se reenvia: %d pistas", d.cuenta(TipoPista))
	}
	if f.lecturasTraz != 1 {
		t.Fatalf("el trazado se lee una vez por simulacion: %d lecturas", f.lecturasTraz)
	}
}

func TestEnCursoLeeElUltimoTramoYLuegoSigueElCursor(t *testing.T) {
	f := &fuenteFalsa{
		haySim:  true,
		sim:     Simulacion{ID: "s1", CarreraID: "9165", Estado: "en_curso", TCarreraMs: 60_000},
		trazado: trazadoPrueba(),
		muestras: []Muestra{
			{Numero: 1, T: 10_000, X: 1, Y: 1}, // viejo: fuera del primer tramo
			{Numero: 1, T: 58_500, X: 2, Y: 2},
			{Numero: 44, T: 59_800, X: 3, Y: 3},
			{Numero: 1, T: 60_700, X: 4, Y: 4},
		},
	}
	l, d, _ := armar(f)
	paso(t, l)
	// Al tomar una carrera en curso solo se manda el ultimo tramo (2 s).
	if got := f.consultas[0]; got != [2]int64{58_000, 60_000} {
		t.Fatalf("primer tramo %v, se esperaba (58000, 60000]", got)
	}
	c := d.ultimas(t)
	if len(c.Muestras) != 2 || c.TCarreraMs != 60_000 || c.SimulacionID != "s1" {
		t.Fatalf("coordenadas inesperadas: %+v", c)
	}

	f.sim.TCarreraMs = 61_000
	paso(t, l)
	if got := f.consultas[1]; got != [2]int64{60_000, 61_000} {
		t.Fatalf("segundo tramo %v, se esperaba (60000, 61000]", got)
	}
	if c := d.ultimas(t); len(c.Muestras) != 1 || c.Muestras[0].T != 60_700 {
		t.Fatalf("el segundo mensaje solo trae lo nuevo: %+v", c.Muestras)
	}

	// Sin avance del reloj no se consulta ni se difunde.
	antes := len(f.consultas)
	paso(t, l)
	if len(f.consultas) != antes {
		t.Fatal("con el reloj quieto no se consulta Mongo")
	}
}

func TestUnAtrasoGrandeSaltaAlPresente(t *testing.T) {
	f := &fuenteFalsa{haySim: true, sim: Simulacion{ID: "s1", CarreraID: "9165", Estado: "en_curso", TCarreraMs: 20_000}, trazado: trazadoPrueba()}
	l, _, _ := armar(f)
	paso(t, l)
	f.sim.TCarreraMs = 80_000 // el Motor estuvo 60 s sin leer
	paso(t, l)
	if got := f.consultas[1]; got != [2]int64{78_000, 80_000} {
		t.Fatalf("tras un atraso se lee solo el ultimo tramo: %v", got)
	}
}

func TestSiMongoFallaNoAvanzaElCursor(t *testing.T) {
	f := &fuenteFalsa{haySim: true, sim: Simulacion{ID: "s1", CarreraID: "9165", Estado: "en_curso", TCarreraMs: 30_000}, trazado: trazadoPrueba()}
	l, _, _ := armar(f)
	paso(t, l)
	f.fallar = true
	f.sim.TCarreraMs = 31_000
	if err := l.Paso(context.Background()); err == nil {
		t.Fatal("se esperaba el error de Mongo")
	}
	f.fallar = false
	f.sim.TCarreraMs = 32_000
	paso(t, l)
	if got := f.consultas[2]; got != [2]int64{30_000, 32_000} {
		t.Fatalf("tras el fallo se relee desde el ultimo tramo difundido: %v", got)
	}
}

func TestUnaSimulacionNuevaReiniciaTodo(t *testing.T) {
	f := &fuenteFalsa{haySim: true, sim: Simulacion{ID: "s1", CarreraID: "9165", Estado: "en_curso", TCarreraMs: 30_000}, trazado: trazadoPrueba()}
	l, d, _ := armar(f)
	paso(t, l)
	f.sim = Simulacion{ID: "s2", CarreraID: "9158", Estado: "en_curso", TCarreraMs: 5_000}
	paso(t, l)
	if f.lecturasTraz != 2 {
		t.Fatalf("cada simulacion lee su trazado: %d lecturas", f.lecturasTraz)
	}
	if d.cuenta(TipoPista) != 2 {
		t.Fatalf("la simulacion nueva difunde su pista de inmediato: %d", d.cuenta(TipoPista))
	}
	if got := f.consultas[1]; got != [2]int64{3_000, 5_000} {
		t.Fatalf("el cursor de la simulacion anterior no se usa: %v", got)
	}
}

func TestSinTrazadoIgualSeMandanLasCoordenadas(t *testing.T) {
	f := &fuenteFalsa{
		haySim:     true,
		sim:        Simulacion{ID: "s1", CarreraID: "9165", Estado: "en_curso", TCarreraMs: 10_000},
		errTrazado: ErrSinTrazado,
		muestras:   []Muestra{{Numero: 1, T: 9_500}},
	}
	l, d, _ := armar(f)
	paso(t, l)
	paso(t, l)
	if d.cuenta(TipoPista) != 0 || d.cuenta(TipoCoordenadas) != 1 {
		t.Fatalf("difundido %v", d.tipos)
	}
	if f.lecturasTraz != 1 {
		t.Fatalf("una carrera sin trazado no se reintenta en cada paso: %d", f.lecturasTraz)
	}
}

func TestSinSimulacionNoDifundeNada(t *testing.T) {
	l, d, _ := armar(&fuenteFalsa{})
	paso(t, l)
	if len(d.tipos) != 0 {
		t.Fatalf("difundido %v", d.tipos)
	}
}
