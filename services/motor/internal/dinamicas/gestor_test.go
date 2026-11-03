package dinamicas

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func cfgPrueba() Config {
	return Config{
		SimulacionID: "11111111-2222-3333-4444-555555555555",
		TotalVueltas: 50,
		Pilotos:      codigos,
		Ventana:      30 * time.Second,
		Observacion:  60 * time.Second,
	}
}

// enVuelta2: parrilla y el lider empezando la vuelta 2 en t = 90 s.
func enVuelta2() []Evento {
	return con(parrilla(0), vta(0, 1, 1, nil), vta(90000, 1, 2, nil))
}

// resumen deja cada accion como "tipo plantilla T [ganadora]" para comparar.
func resumen(acc []Accion) []string {
	var r []string
	for _, a := range acc {
		s := string(a.Tipo) + " " + a.Mercado.Plantilla + " " + strconv.FormatInt(a.T, 10)
		if a.Tipo == AccionResolver {
			s += " " + a.Ganadora
		}
		r = append(r, s)
	}
	return r
}

func correr(cfg Config, evs []Evento, reloj int64) []Accion {
	return NuevoGestor(cfg).Avanzar(evs, reloj)
}

func TestVueltasPermitidas(t *testing.T) {
	casos := []struct {
		nombre string
		vuelta int
		abre   bool
	}{
		{"vuelta 1", 1, false},
		{"vuelta 2", 2, true},
		{"antepenultima (48 de 50)", 48, true},
		{"penultima (49 de 50)", 49, false},
		{"ultima (50 de 50)", 50, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			evs := con(parrilla(0), vta(1000, 1, c.vuelta, nil), intv(2000, 4, f(0.5)))
			acc := correr(cfgPrueba(), evs, 2000)
			if (len(acc) > 0) != c.abre {
				t.Fatalf("acciones = %v", resumen(acc))
			}
		})
	}
	t.Run("sin total de vueltas no abre", func(t *testing.T) {
		cfg := cfgPrueba()
		cfg.TotalVueltas = 0
		if acc := correr(cfg, con(enVuelta2(), intv(100000, 4, f(0.5))), 100000); len(acc) != 0 {
			t.Fatalf("acciones = %v", resumen(acc))
		}
	})
}

func TestNoAbreAntesDelPuntoDeInicio(t *testing.T) {
	cfg := cfgPrueba()
	cfg.InicioMs = 200000
	evs := con(enVuelta2(), intv(150000, 4, f(0.5)), intv(200000, 4, f(0.5)))
	got := resumen(correr(cfg, evs, 200000))
	if want := []string{"crear cambio_lider 200000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("acciones = %v", got)
	}
}

func TestPrioridad(t *testing.T) {
	// Todo el estado se arma antes de InicioMs; el evento en InicioMs evalua.
	const t0 = 100000
	cambio := intv(10, 4, f(0.5))  // NOR a 0,5 s de VER
	adel := intv(10, 16, f(0.4))   // LEC a 0,4 s de NOR
	brecha := intv(10, 44, f(2.0)) // 44 a 2 s de LEC
	pits := stintEv(10, 1, 1, 0)   // VER en vuelta 19 con las de la largada
	base := con(parrilla(0), pos(0, 44, 4), vta(0, 4, 1, f(80)), vta(50000, 1, 19, nil))
	neutro := pos(t0, 1, 1)

	casos := []struct {
		nombre string
		evs    []Evento
		gana   string
	}{
		{"safety_car gana a todas", con(base, cambio, adel, brecha, pits, ctrl(t0, "YELLOW", "")), PlantillaSafetyCar},
		{"cambio_lider", con(base, cambio, adel, brecha, pits, neutro), PlantillaCambioLider},
		{"adelantamiento", con(base, adel, brecha, pits, neutro), PlantillaAdelantamiento},
		{"entrada_pits", con(base, brecha, pits, neutro), PlantillaEntradaPits},
		{"brecha_se_cierra", con(base, brecha, vta(t0, 1, 20, nil)), PlantillaBrecha},
		{"vuelta_rapida", con(base, vta(t0, 1, 20, nil)), PlantillaVueltaRapida},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cfg := cfgPrueba()
			cfg.InicioMs = t0
			acc := correr(cfg, c.evs, t0)
			if len(acc) != 1 || acc[0].Tipo != AccionCrear || acc[0].Mercado.Plantilla != c.gana {
				t.Fatalf("acciones = %v", resumen(acc))
			}
		})
	}
}

func TestUnMercadoVivoYPausaTrasResolver(t *testing.T) {
	evs := con(enVuelta2(),
		intv(100000, 4, f(0.5)),  // abre cambio_lider; cierra en 130 s y resuelve en 190 s
		intv(110000, 16, f(0.3)), // adelantamiento, pero hay una abierta
		intv(150000, 16, f(0.3)), // cerrada pero observando: tampoco
		intv(195000, 16, f(0.3)), // resuelta hace 5 s: pausa
		intv(200000, 16, f(0.3)), // 10 s despues de resolver: abre
	)
	got := resumen(correr(cfgPrueba(), evs, 200000))
	want := []string{
		"crear cambio_lider 100000", "cerrar cambio_lider 130000",
		"resolver cambio_lider 190000 No", "crear adelantamiento 200000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acciones = %v", got)
	}
}

func TestCooldownPlantilla(t *testing.T) {
	inicio := []string{"crear cambio_lider 100000", "cerrar cambio_lider 130000", "resolver cambio_lider 190000 No"}
	casos := []struct {
		nombre string
		t      int64
		abre   []string
	}{
		// En 250 s NOR pasa a liderar con VER a 0,5 s (otro lider, asi no lo
		// frena el cooldown por lider). P1-P2 no es adelantamiento: mientras
		// dure el cooldown de la plantilla no se abre nada.
		{"a los 180 s menos 1 ms no abre", 279999, nil},
		{"a los 180 s vuelve a abrir", 280000, []string{"crear cambio_lider 280000"}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			evs := con(enVuelta2(), intv(100000, 4, f(0.5)), pos(250000, 4, 1), pos(250000, 1, 2), intv(c.t, 1, f(0.5)))
			got := resumen(correr(cfgPrueba(), evs, c.t))
			if want := append(append([]string(nil), inicio...), c.abre...); !reflect.DeepEqual(got, want) {
				t.Fatalf("acciones = %v", got)
			}
		})
	}
}

func TestCambioLiderNoRepiteLiderAntesDe600s(t *testing.T) {
	// Solo se cumple cambio_lider (NOR a 0,5 s de VER todo el tiempo).
	evs := con(enVuelta2(), intv(100000, 4, f(0.5)), intv(280000, 4, f(0.5)), intv(699999, 4, f(0.5)), intv(700000, 4, f(0.5)))
	var creados []string
	for _, a := range resumen(correr(cfgPrueba(), evs, 700000)) {
		if strings.HasPrefix(a, "crear") {
			creados = append(creados, a)
		}
	}
	if want := []string{"crear cambio_lider 100000", "crear cambio_lider 700000"}; !reflect.DeepEqual(creados, want) {
		t.Fatalf("creados = %v", creados)
	}
}

// carreraDePelea: 5 pilotos a 0,5 s uno de otro todo el tiempo, asi que
// cambio_lider y adelantamiento se cumplen siempre. El lider rota cada 200 s
// (en 150 s, 350 s, ...) para que cambio_lider no lo frene el cooldown por
// lider.
func carreraDePelea(hasta int64) []Evento {
	pilotos := []int{1, 4, 16, 44, 63}
	var evs []Evento
	for i, p := range pilotos {
		evs = append(evs, pos(0, p, i+1), vta(0, p, 2, nil))
	}
	for k := int64(0); 150000+k*200000 < hasta; k++ {
		t := 150000 + k*200000
		for i, p := range pilotos {
			// rotacion: el que iba en P(k+1) queda de lider
			evs = append(evs, pos(t, p, (i-int(k+1)%5+5)%5+1))
		}
	}
	for t := int64(1000); t <= hasta; t += 1000 {
		for _, p := range pilotos {
			evs = append(evs, intv(t, p, f(0.5)))
		}
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].T != evs[j].T {
			return evs[i].T < evs[j].T
		}
		return evs[i].ID < evs[j].ID
	})
	return evs
}

func TestVariedadSeAlternan(t *testing.T) {
	cfg := cfgPrueba()
	cfg.TotalVueltas = 100
	evs := carreraDePelea(1_000_000)
	acciones := correr(cfg, evs, 1_000_000)
	var plantillas []string
	for _, a := range acciones {
		if a.Tipo == AccionCrear {
			plantillas = append(plantillas, a.Mercado.Plantilla)
		}
	}
	t.Logf("secuencia: %v", plantillas)
	if len(plantillas) < 8 {
		t.Fatalf("muy pocos mercados: %v", plantillas)
	}
	for i := 1; i < len(plantillas); i++ {
		if plantillas[i] == plantillas[i-1] {
			t.Fatalf("no se alternan: %v", plantillas)
		}
	}
	for _, p := range plantillas {
		if p != PlantillaCambioLider && p != PlantillaAdelantamiento {
			t.Fatalf("plantilla inesperada %s: %v", p, plantillas)
		}
	}
	// Determinista: otra corrida da lo mismo.
	if otra := correr(cfg, evs, 1_000_000); !reflect.DeepEqual(otra, acciones) {
		t.Fatal("dos corridas difieren")
	}
}

func TestVariedadGanaLaMasAntigua(t *testing.T) {
	// adelantamiento abre en 100 s, cambio_lider en 200 s y brecha en 300 s.
	// Durante la brecha (350 s) NOR pasa a liderar y se cumplen a la vez
	// cambio_lider (NOR) y adelantamiento (VER-LEC). Al terminar la pausa
	// (400 s) las dos estan disponibles: gana adelantamiento, que lleva mas
	// tiempo sin abrirse, aunque cambio_lider tenga mas prioridad.
	evs := con(parrilla(0), pos(0, 44, 4), vta(0, 1, 2, nil), intv(0, 4, f(5)),
		intv(100000, 16, f(0.5)),
		intv(200000, 4, f(0.5)),
		intv(200500, 4, f(5)), intv(200500, 16, f(5)),
		intv(300000, 44, f(2.0)),
		pos(350000, 4, 1), pos(350000, 1, 2), intv(350000, 1, f(0.5)), intv(350000, 16, f(0.5)),
		pos(400000, 44, 4),
	)
	var creados []string
	for _, a := range resumen(correr(cfgPrueba(), evs, 400000)) {
		if strings.HasPrefix(a, "crear") {
			creados = append(creados, a)
		}
	}
	want := []string{"crear adelantamiento 100000", "crear cambio_lider 200000", "crear brecha_se_cierra 300000", "crear adelantamiento 400000"}
	if !reflect.DeepEqual(creados, want) {
		t.Fatalf("creados = %v", creados)
	}
}

func TestVariedadEmpateYSafetyCar(t *testing.T) {
	// Nunca abiertas las dos: empate, gana la prioridad (cambio_lider).
	base := con(parrilla(0), vta(0, 1, 2, nil), intv(10, 4, f(0.5)), intv(10, 16, f(0.4)))
	cfg := cfgPrueba()
	cfg.InicioMs = 100000
	if acc := correr(cfg, con(base, pos(100000, 1, 1)), 100000); acc[0].Mercado.Plantilla != PlantillaCambioLider {
		t.Fatalf("acciones = %v", resumen(acc))
	}
	// safety_car gana aunque las otras lleven mas tiempo sin abrirse.
	if acc := correr(cfg, con(base, ctrl(100000, "YELLOW", "")), 100000); acc[0].Mercado.Plantilla != PlantillaSafetyCar {
		t.Fatalf("acciones = %v", resumen(acc))
	}
}

func TestCooldownPar(t *testing.T) {
	base := con(enVuelta2(), pos(0, 44, 4), intv(100000, 16, f(0.5))) // adelantamiento LEC-NOR
	casos := []struct {
		nombre string
		evs    []Evento
		ultima string
	}{
		{"mismo par en otra plantilla espera", con(base, intv(200000, 16, f(2.0))), "resolver adelantamiento 190000 No"},
		{"otro par no espera", con(base, intv(200000, 16, nil), intv(200000, 44, f(2.0))), "crear brecha_se_cierra 200000"},
		{"mismo par a los 300 s", con(base, intv(399999, 16, f(2.0)), intv(400000, 16, f(2.0))), "crear brecha_se_cierra 400000"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := resumen(correr(cfgPrueba(), c.evs, c.evs[len(c.evs)-1].T))
			if got[len(got)-1] != c.ultima {
				t.Fatalf("acciones = %v", got)
			}
		})
	}
}

func TestVentanaDeObservacion(t *testing.T) {
	// adelantamiento LEC-NOR abre en 100 s, cierra en 130 s, resuelve en 190 s.
	// (Cuando LEC pasa queda a 0,5 s de VER y despues abre cambio_lider.)
	base := con(enVuelta2(), intv(100000, 16, f(0.5)))
	casos := []struct {
		nombre string
		paso   int64 // LEC pasa a NOR en este t
		reloj  int64
		res    string // "" = no se resuelve
	}{
		{"pasa en la observacion", 150000, 200000, "resolver adelantamiento 190000 Sí"},
		{"pasa justo en race_close+O", 190000, 200000, "resolver adelantamiento 190000 Sí"},
		{"pasa despues", 190001, 200000, "resolver adelantamiento 190000 No"},
		{"el reloj no llega", 150000, 189999, ""},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			evs := con(base, pos(c.paso, 16, 2), pos(c.paso, 4, 3))
			var lote []Evento
			for _, ev := range evs {
				if ev.T <= c.reloj {
					lote = append(lote, ev)
				}
			}
			got := resumen(correr(cfgPrueba(), lote, c.reloj))
			res := ""
			for _, a := range got {
				if strings.HasPrefix(a, "resolver adelantamiento") {
					res = a
				}
			}
			if res != c.res {
				t.Fatalf("acciones = %v", got)
			}
		})
	}
}

func TestVueltaSoloCuentaAlTerminar(t *testing.T) {
	g := NuevoGestor(cfgPrueba())
	g.Avanzar([]Evento{vta(1000, 4, 1, f(90))}, 90999)
	if _, ok := g.estado.MejorVuelta(); ok {
		t.Fatal("la vuelta termina en 91 s: aun no existe")
	}
	g.Avanzar(nil, 91000)
	if m, ok := g.estado.MejorVuelta(); !ok || m != 90 {
		t.Fatalf("mejor = %v, %v", m, ok)
	}
}

func TestVueltaRapida(t *testing.T) {
	// VER empieza la 10 en 120 s: cierra en 150 s y resuelve en 210 s.
	// M = 90 (NOR, terminada en 91 s). La vuelta de 70 s de LEC, empezada en
	// 100 s, termina en 170 s: no cuenta para M pero si para resolver.
	base := con(parrilla(0), vta(1000, 4, 1, f(90)), vta(50000, 1, 9, nil), vta(100000, 16, 9, f(70)), vta(120000, 1, 10, nil))
	got := resumen(correr(cfgPrueba(), base, 210000))
	want := []string{"crear vuelta_rapida 120000", "cerrar vuelta_rapida 150000", "resolver vuelta_rapida 210000 Sí"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acciones = %v", got)
	}
	acc := correr(cfgPrueba(), base, 120000)
	if acc[0].Mercado.MejorVuelta != 90 {
		t.Fatalf("M = %v: conto una vuelta que no habia terminado", acc[0].Mercado.MejorVuelta)
	}

	// Si la vuelta rapida termina despues de la observacion, gana No.
	tarde := con(parrilla(0), vta(1000, 4, 1, f(90)), vta(50000, 1, 9, nil), vta(160000, 16, 9, f(70)), vta(120000, 1, 10, nil))
	sort.SliceStable(tarde, func(i, j int) bool { return tarde[i].T < tarde[j].T })
	got = resumen(correr(cfgPrueba(), tarde, 240000))
	if got[len(got)-1] != "resolver vuelta_rapida 210000 No" {
		t.Fatalf("acciones = %v", got)
	}
}

func TestVueltaRapidaUsaLaMDelCierre(t *testing.T) {
	// Abre en 120 s con M = 90 (NOR). Durante la ventana (120, 150] termina
	// una de 85 s de LEC (empezada en 55 s): M del cierre = 85. En la
	// observacion termina una de 87 s de 44 (empezada en 83 s, fin 170 s):
	// baja la M de la apertura pero no la del cierre => No.
	evs := con(parrilla(0), vta(1000, 4, 1, f(90)), vta(50000, 1, 9, nil), vta(55000, 16, 9, f(85)),
		vta(83000, 44, 9, f(87)), vta(120000, 1, 10, nil))
	acc := correr(cfgPrueba(), evs, 210000)
	got := resumen(acc)
	want := []string{"crear vuelta_rapida 120000", "cerrar vuelta_rapida 150000", "resolver vuelta_rapida 210000 No"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acciones = %v", got)
	}
	if acc[0].Mercado.MejorVuelta != 90 || acc[1].Mercado.MejorVueltaCierre != 85 {
		t.Fatalf("M apertura = %v, M cierre = %v", acc[0].Mercado.MejorVuelta, acc[1].Mercado.MejorVueltaCierre)
	}
}

func TestEntradaPitsUsaElTop10DelCierre(t *testing.T) {
	evs := con(enVuelta2(), stintEv(0, 16, 1, 0), vta(95000, 16, 19, nil), vta(96000, 1, 19, nil),
		pitEv(140000, 16))
	got := resumen(correr(cfgPrueba(), evs, 200000))
	want := []string{"crear entrada_pits 95000", "cerrar entrada_pits 125000", "resolver entrada_pits 185000 Sí"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acciones = %v", got)
	}
}

func TestSafetyCar(t *testing.T) {
	evs := con(enVuelta2(), ctrl(100000, "DOUBLE YELLOW", "DOUBLE YELLOW IN TRACK SECTOR 7"),
		ctrl(160000, "", "VIRTUAL SAFETY CAR DEPLOYED"))
	got := resumen(correr(cfgPrueba(), evs, 190000))
	if got[len(got)-1] != "resolver safety_car 190000 Sí" {
		t.Fatalf("acciones = %v", got)
	}
}

func TestTerminar(t *testing.T) {
	casos := []struct {
		nombre string
		reloj  int64
		cierra []string
	}{
		// cambio_lider abre en 100 s, cierra en 130 s y resuelve en 190 s.
		{"termina con el mercado abierto: se cierra", 110000, []string{"cerrar cambio_lider 110000"}},
		{"termina observando: queda cerrado sin resolver", 150000, nil},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			g := NuevoGestor(cfgPrueba())
			g.Avanzar(con(enVuelta2(), intv(100000, 4, f(0.5))), c.reloj)
			acc, sinResolver := g.Terminar()
			if got := resumen(acc); !reflect.DeepEqual(got, c.cierra) {
				t.Fatalf("acciones = %v", got)
			}
			if len(sinResolver) != 1 || sinResolver[0].Plantilla != PlantillaCambioLider {
				t.Fatalf("sin resolver = %+v", sinResolver)
			}
		})
	}
	g := NuevoGestor(cfgPrueba())
	g.Avanzar(con(enVuelta2(), intv(100000, 4, f(0.5))), 150000)
	g.Terminar()
	if acc := g.Avanzar([]Evento{pos(300000, 4, 1)}, 400000); acc != nil {
		t.Fatalf("despues de terminar no hay acciones: %v", resumen(acc))
	}
}

func TestDescartaEventosYaPasados(t *testing.T) {
	g := NuevoGestor(cfgPrueba())
	g.Avanzar(enVuelta2(), 100000)
	g.Avanzar([]Evento{intv(100000, 4, f(0.5)), intv(99000, 4, f(0.5))}, 110000)
	if g.Descartados() != 2 {
		t.Fatalf("descartados = %d", g.Descartados())
	}
}

func TestMercadoCreado(t *testing.T) {
	cfg := cfgPrueba()
	acc := correr(cfg, con(enVuelta2(), intv(100000, 4, f(0.5))), 100000)
	m := acc[0].Mercado
	if acc[0].Estado != EstadoAbierto || m.EventoID != cfg.SimulacionID || m.T0 != 100000 ||
		m.Cierre != 130000 || m.Resolucion != 190000 || m.ID != IDMercado(cfg.SimulacionID, PlantillaCambioLider, 100000) {
		t.Fatalf("mercado = %+v", m)
	}
}

// carreraSintetica arma una carrera de 30 vueltas con 5 pilotos, con
// intervalos que oscilan, adelantamientos, amarillas, un SC y un pit, para
// que salgan mercados de varias plantillas. Sin rand: todo es aritmetica.
func carreraSintetica() []Evento {
	pilotos := []int{1, 4, 16, 44, 63}
	var evs []Evento
	for i, p := range pilotos {
		evs = append(evs, pos(-5000, p, i+1), stintEv(0, p, 1, 0))
	}
	for v := 1; v <= 30; v++ {
		for i, p := range pilotos {
			inicio := int64(v-1)*90000 + int64(i)*800
			dur := 89.0 + float64((p*7+v*13)%30)/10
			evs = append(evs, vta(inicio, p, v, f(dur)))
		}
	}
	for t := int64(0); t < 30*90000; t += 4000 {
		for i, p := range pilotos[1:] {
			v := 0.2 + float64((t/4000+int64(i)*3)%35)/10
			var iv *float64
			if (t/4000+int64(i))%11 != 0 { // de vez en cuando "+1 LAP" o null
				iv = f(v)
			}
			evs = append(evs, intv(t+int64(i)*100+50, p, iv))
		}
	}
	for v := 3; v <= 28; v += 5 { // NOR y LEC cambian de puesto cada 5 vueltas
		t := int64(v)*90000 + 45000
		if v%2 == 1 {
			evs = append(evs, pos(t, 16, 2), pos(t, 4, 3))
		} else {
			evs = append(evs, pos(t, 4, 2), pos(t, 16, 3))
		}
	}
	evs = append(evs,
		ctrl(5*90000+10000, "YELLOW", "YELLOW IN TRACK SECTOR 3"),
		ctrl(12*90000+3000, "DOUBLE YELLOW", "DOUBLE YELLOW IN TRACK SECTOR 9"),
		ctrl(12*90000+20000, "", "SAFETY CAR DEPLOYED"),
		ctrl(21*90000, "YELLOW", "YELLOW IN TRACK SECTOR 1"),
		pitEv(19*90000+60000, 16), stintEv(20*90000, 16, 20, 0),
	)
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].T != evs[j].T {
			return evs[i].T < evs[j].T
		}
		return evs[i].ID < evs[j].ID
	})
	return evs
}

func TestDeterminismo(t *testing.T) {
	evs := carreraSintetica()
	final := evs[len(evs)-1].T + 120000
	cfg := cfgPrueba()
	cfg.TotalVueltas = 30

	todo := correr(cfg, evs, final)

	// Contra si mismo: otra corrida desde cero.
	if otra := correr(cfg, evs, final); !reflect.DeepEqual(todo, otra) {
		t.Fatal("dos corridas iguales dieron acciones distintas")
	}

	// Evento por evento (agrupando los del mismo t, como hace (cursor, reloj]).
	g := NuevoGestor(cfg)
	var deAUno []Accion
	for i := 0; i < len(evs); {
		j := i
		for j < len(evs) && evs[j].T == evs[i].T {
			j++
		}
		deAUno = append(deAUno, g.Avanzar(evs[i:j], evs[i].T)...)
		i = j
	}
	deAUno = append(deAUno, g.Avanzar(nil, final)...)
	if !reflect.DeepEqual(todo, deAUno) {
		t.Fatalf("de a uno difiere:\n%v\n%v", resumen(todo), resumen(deAUno))
	}

	// Lotes de tamano variable con el reloj entre eventos (como los ticks).
	g = NuevoGestor(cfg)
	var porLotes []Accion
	for i, n := 0, 1; i < len(evs); n = n%97 + 13 {
		j := min(i+n, len(evs))
		for j < len(evs) && evs[j].T == evs[j-1].T {
			j++
		}
		reloj := evs[j-1].T
		if j < len(evs) {
			reloj = (evs[j-1].T + evs[j].T - 1) / 2
			if reloj < evs[j-1].T {
				reloj = evs[j-1].T
			}
		}
		porLotes = append(porLotes, g.Avanzar(evs[i:j], reloj)...)
		i = j
	}
	porLotes = append(porLotes, g.Avanzar(nil, final)...)
	if !reflect.DeepEqual(todo, porLotes) {
		t.Fatalf("por lotes difiere:\n%v\n%v", resumen(todo), resumen(porLotes))
	}

	// La carrera debe ejercitar el ciclo completo y varias plantillas.
	plantillas := map[string]bool{}
	cuenta := map[TipoAccion]int{}
	vivos := 0 // abiertos u observando
	var ultimaResolucion int64 = -1
	for _, a := range todo {
		cuenta[a.Tipo]++
		switch a.Tipo {
		case AccionCrear:
			plantillas[a.Mercado.Plantilla] = true
			vivos++
			if vivos > 1 {
				t.Fatalf("dos mercados vivos a la vez en %d", a.T)
			}
			if ultimaResolucion >= 0 && a.T-ultimaResolucion < pausaTrasCierreMs {
				t.Fatalf("abrio %d ms despues de la resolucion", a.T-ultimaResolucion)
			}
		case AccionResolver:
			vivos--
			ultimaResolucion = a.T
		}
	}
	if cuenta[AccionCrear] < 5 || cuenta[AccionCrear] != cuenta[AccionCerrar] || cuenta[AccionCrear] != cuenta[AccionResolver] {
		t.Fatalf("cuenta = %v", cuenta)
	}
	if len(plantillas) < 4 {
		t.Fatalf("pocas plantillas: %v", plantillas)
	}
	t.Logf("%d mercados, plantillas %v", cuenta[AccionCrear], plantillas)
}
