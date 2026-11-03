package dinamicas

import (
	"reflect"
	"testing"
)

func con(base []Evento, mas ...Evento) []Evento {
	return append(append([]Evento(nil), base...), mas...)
}

func TestEvaluarPlantillas(t *testing.T) {
	g := parrilla(0)
	llantas := con(g, stintEv(0, 16, 1, 0), vta(0, 16, 19, nil))
	var diez []Evento // diez pilotos delante de LEC
	for i := 1; i <= 10; i++ {
		diez = append(diez, pos(0, 100+i, i))
	}
	casos := []struct {
		nombre   string
		pl       Plantilla
		evs      []Evento
		ev       Evento // evento del instante; si es cero se usa el ultimo
		dispara  bool
		pilotos  []int
		pregunta string
	}{
		{"adelantamiento dispara", adelantamiento{}, con(g, intv(1, 16, f(0.6))), Evento{}, true, []int{4, 16},
			"¿LEC irá delante de NOR al terminar el próximo minuto?"},
		{"adelantamiento elige el menor intervalo", adelantamiento{}, con(g, intv(1, 4, f(0.9)), intv(1, 16, f(0.4))), Evento{}, true, []int{4, 16}, ""},
		{"adelantamiento empate por posicion", adelantamiento{}, con(g, intv(1, 4, f(0.5)), intv(1, 16, f(0.5))), Evento{}, true, []int{1, 4}, ""},
		{"adelantamiento justo en 1.0 no", adelantamiento{}, con(g, intv(1, 16, f(1.0))), Evento{}, false, nil, ""},
		{"adelantamiento con intervalo null no", adelantamiento{}, con(g, intv(1, 16, nil)), Evento{}, false, nil, ""},
		{"adelantamiento sin posiciones no", adelantamiento{}, []Evento{intv(1, 16, f(0.3))}, Evento{}, false, nil, ""},

		{"cambio_lider dispara", cambioLider{}, con(g, intv(1, 4, f(1.2))), Evento{}, true, []int{1},
			"¿Habrá un líder distinto de VER al terminar el próximo minuto?"},
		{"cambio_lider lejos no", cambioLider{}, con(g, intv(1, 4, f(1.5))), Evento{}, false, nil, ""},
		{"cambio_lider texto no", cambioLider{}, con(g, intv(1, 4, nil)), Evento{}, false, nil, ""},

		{"brecha dispara", brechaSeCierra{}, con(g, intv(1, 16, f(2.2))), Evento{}, true, []int{4, 16},
			"¿LEC estará a menos de 1 s de NOR al terminar el próximo minuto?"},
		{"brecha en los bordes", brechaSeCierra{}, con(g, intv(1, 4, f(3.0)), intv(1, 16, f(1.0))), Evento{}, true, []int{4, 16}, ""},
		{"brecha muy cerca no", brechaSeCierra{}, con(g, intv(1, 16, f(0.9))), Evento{}, false, nil, ""},
		{"brecha muy lejos no", brechaSeCierra{}, con(g, intv(1, 16, f(3.1))), Evento{}, false, nil, ""},

		{"pits dispara con 18 vueltas", entradaPits{}, llantas, Evento{}, true, nil,
			"¿Algún piloto del top 10 entrará a pits en el próximo minuto?"},
		{"pits con 17 vueltas no", entradaPits{}, con(g, stintEv(0, 16, 2, 0), vta(0, 16, 19, nil)), Evento{}, false, nil, ""},
		{"pits fuera del top 10 no", entradaPits{}, con(diez, pos(0, 16, 11), stintEv(0, 16, 1, 0), vta(0, 16, 19, nil)), Evento{}, false, nil, ""},

		{"vuelta_rapida en la 10", vueltaRapida{}, []Evento{terminada(1, 4, 91.2), vta(2, 1, 10, nil)}, Evento{}, true, nil,
			"¿Habrá una nueva vuelta rápida de la carrera en el próximo minuto?"},
		{"vuelta_rapida en la 11 no", vueltaRapida{}, []Evento{terminada(1, 4, 91.2), vta(2, 1, 11, nil)}, Evento{}, false, nil, ""},
		{"vuelta_rapida sin vuelta terminada no", vueltaRapida{}, []Evento{vta(2, 1, 10, f(90))}, Evento{}, false, nil, ""},
		{"vuelta_rapida no la dispara el segundo en llegar", vueltaRapida{}, []Evento{terminada(1, 4, 91.2), vta(2, 1, 10, nil), vta(3, 4, 10, nil)}, Evento{}, false, nil, ""},

		{"safety_car con amarilla", safetyCar{}, nil, ctrl(1, "YELLOW", "YELLOW IN TRACK SECTOR 4"), true, nil,
			"¿Saldrá el Safety Car o el VSC en el próximo minuto?"},
		{"safety_car con doble amarilla", safetyCar{}, nil, ctrl(1, "DOUBLE YELLOW", ""), true, nil, ""},
		{"safety_car con verde no", safetyCar{}, nil, ctrl(1, "GREEN", "TRACK CLEAR"), false, nil, ""},
		{"safety_car sin bandera no", safetyCar{}, nil, ctrl(1, "", "SAFETY CAR DEPLOYED"), false, nil, ""},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			e := estadoCon(c.evs...)
			ev := c.ev
			if ev.Tipo == "" && len(c.evs) > 0 {
				ev = c.evs[len(c.evs)-1]
			} else if ev.Tipo != "" {
				e.Aplicar(ev)
			}
			props := c.pl.Evaluar(e, ev)
			if (len(props) > 0) != c.dispara {
				t.Fatalf("dispara = %v, se esperaba %v (%+v)", len(props) > 0, c.dispara, props)
			}
			if !c.dispara {
				return
			}
			p := props[0]
			if p.Plantilla != c.pl.ID() || !reflect.DeepEqual(p.Pilotos, c.pilotos) {
				t.Errorf("propuesta = %+v", p)
			}
			if len(p.Opciones) != 2 || p.Opciones[0].Nombre != OpcionSi || p.Opciones[1].Nombre != OpcionNo {
				t.Errorf("opciones = %+v", p.Opciones)
			}
			if c.pregunta != "" && p.Pregunta != c.pregunta {
				t.Errorf("pregunta = %q", p.Pregunta)
			}
		})
	}
}

func TestVueltaRapidaGuardaM(t *testing.T) {
	e := estadoCon(terminada(1, 4, 91.2), terminada(2, 1, 90.5), vta(3, 1, 20, nil))
	props := vueltaRapida{}.Evaluar(e, vta(3, 1, 20, nil))
	if len(props) != 1 || props[0].MejorVuelta != 90.5 {
		t.Fatalf("props = %+v", props)
	}
}

func TestResolverPlantillas(t *testing.T) {
	par := func(pl string, a, b int) Mercado {
		return Mercado{Propuesta: Propuesta{Plantilla: pl, Pilotos: []int{a, b}}}
	}
	casos := []struct {
		nombre string
		pl     Plantilla
		m      Mercado
		fin    []Evento // estado al final de la observacion
		obs    []Evento
		gana   string
	}{
		{"adelantamiento si", adelantamiento{}, par(PlantillaAdelantamiento, 4, 16), con(parrilla(0), pos(5, 16, 2), pos(5, 4, 3)), nil, OpcionSi},
		{"adelantamiento no", adelantamiento{}, par(PlantillaAdelantamiento, 4, 16), parrilla(0), nil, OpcionNo},

		{"cambio_lider si", cambioLider{}, Mercado{Propuesta: Propuesta{Pilotos: []int{1}}}, con(parrilla(0), pos(5, 4, 1), pos(5, 1, 2)), nil, OpcionSi},
		{"cambio_lider no", cambioLider{}, Mercado{Propuesta: Propuesta{Pilotos: []int{1}}}, parrilla(0), nil, OpcionNo},

		{"brecha si: lo paso", brechaSeCierra{}, par(PlantillaBrecha, 4, 16), con(parrilla(0), pos(5, 16, 2), pos(5, 4, 3)), nil, OpcionSi},
		{"brecha si: a menos de 1 s", brechaSeCierra{}, par(PlantillaBrecha, 4, 16), con(parrilla(0), intv(5, 16, f(0.8))), nil, OpcionSi},
		{"brecha no: sigue lejos", brechaSeCierra{}, par(PlantillaBrecha, 4, 16), con(parrilla(0), intv(5, 16, f(1.7))), nil, OpcionNo},
		{"brecha no: intervalo null", brechaSeCierra{}, par(PlantillaBrecha, 4, 16), con(parrilla(0), intv(5, 16, nil)), nil, OpcionNo},

		{"pits si", entradaPits{}, Mercado{OrdenCierre: []int{1, 4, 16}}, nil, []Evento{pitEv(5, 16)}, OpcionSi},
		{"pits no: nadie entra", entradaPits{}, Mercado{OrdenCierre: []int{1, 4, 16}}, nil, []Evento{intv(5, 16, f(1))}, OpcionNo},
		{"pits no: entra alguien fuera del top 10", entradaPits{}, Mercado{OrdenCierre: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}, nil, []Evento{pitEv(5, 11)}, OpcionNo},

		{"vuelta_rapida si", vueltaRapida{}, Mercado{Propuesta: Propuesta{MejorVuelta: 90.5}}, nil, []Evento{terminada(5, 4, 90.4)}, OpcionSi},
		{"vuelta_rapida no: igual a M", vueltaRapida{}, Mercado{Propuesta: Propuesta{MejorVuelta: 90.5}}, nil, []Evento{terminada(5, 4, 90.5)}, OpcionNo},
		// El inicio de una vuelta rapida no cuenta: solo cuando termina.
		{"vuelta_rapida no: solo empezo", vueltaRapida{}, Mercado{Propuesta: Propuesta{MejorVuelta: 90.5}}, nil, []Evento{vta(5, 4, 12, f(80))}, OpcionNo},

		{"safety_car si: SC", safetyCar{}, Mercado{}, nil, []Evento{ctrl(5, "", "SAFETY CAR DEPLOYED")}, OpcionSi},
		{"safety_car si: VSC", safetyCar{}, Mercado{}, nil, []Evento{ctrl(5, "", "VIRTUAL SAFETY CAR DEPLOYED")}, OpcionSi},
		{"safety_car no", safetyCar{}, Mercado{}, nil, []Evento{ctrl(5, "YELLOW", "YELLOW IN TRACK SECTOR 4")}, OpcionNo},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := c.pl.Resolver(c.m, estadoCon(c.fin...), c.obs); got != c.gana {
				t.Fatalf("gana %q, se esperaba %q", got, c.gana)
			}
		})
	}
}
