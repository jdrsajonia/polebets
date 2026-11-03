package dinamicas

import "testing"

func TestNormalizar(t *testing.T) {
	casos := []struct {
		nombre string
		tipo   string
		raw    map[string]any
		ok     bool
		check  func(t *testing.T, ev Evento)
	}{
		{"posicion con double", "posicion", map[string]any{"driver_number": 1.0, "position": 2.0}, true,
			func(t *testing.T, ev Evento) {
				if ev.Piloto != 1 || ev.Posicion != 2 {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"posicion con int32 de bson", "posicion", map[string]any{"driver_number": int32(44), "position": int32(5)}, true,
			func(t *testing.T, ev Evento) {
				if ev.Piloto != 44 || ev.Posicion != 5 {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"posicion sin position", "posicion", map[string]any{"driver_number": 1.0, "position": nil}, false, nil},
		{"sin driver_number", "intervalo", map[string]any{"interval": 0.4}, false, nil},
		{"intervalo numerico", "intervalo", map[string]any{"driver_number": 4.0, "interval": 0.75, "gap_to_leader": 3.2}, true,
			func(t *testing.T, ev Evento) {
				if ev.Intervalo == nil || *ev.Intervalo != 0.75 || ev.BrechaLider == nil || *ev.BrechaLider != 3.2 {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"intervalo null", "intervalo", map[string]any{"driver_number": 4.0, "interval": nil, "gap_to_leader": nil}, true,
			func(t *testing.T, ev Evento) {
				if ev.Intervalo != nil || ev.BrechaLider != nil {
					t.Errorf("null debe quedar sin dato: %+v", ev)
				}
			}},
		{"intervalo con texto +1 LAP", "intervalo", map[string]any{"driver_number": 4.0, "interval": "+1 LAP", "gap_to_leader": "+1 LAP"}, true,
			func(t *testing.T, ev Evento) {
				if ev.Intervalo != nil || ev.BrechaLider != nil {
					t.Errorf("texto debe quedar sin dato: %+v", ev)
				}
			}},
		{"vuelta sin duracion", "vuelta", map[string]any{"driver_number": 1.0, "lap_number": 1.0, "lap_duration": nil}, true,
			func(t *testing.T, ev Evento) {
				if ev.Vuelta != 1 || ev.DuracionVuelta != nil {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"stint ignora lap_end", "stint", map[string]any{"driver_number": 16.0, "lap_start": 12.0, "lap_end": 30.0, "tyre_age_at_start": 3.0, "compound": "HARD"}, true,
			func(t *testing.T, ev Evento) {
				if ev.VueltaInicio != 12 || ev.EdadInicial != 3 || ev.Compuesto != "HARD" {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"stint con edad null", "stint", map[string]any{"driver_number": 16.0, "lap_start": 1.0, "tyre_age_at_start": nil}, true,
			func(t *testing.T, ev Evento) {
				if ev.EdadInicial != 0 {
					t.Errorf("edad = %d", ev.EdadInicial)
				}
			}},
		{"pit", "pit", map[string]any{"driver_number": 4.0, "pit_duration": 22.5}, true,
			func(t *testing.T, ev Evento) {
				if ev.Piloto != 4 {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"control normaliza la bandera", "control", map[string]any{"category": "Flag", "flag": "double yellow ", "message": "DOUBLE YELLOW IN SECTOR 2"}, true,
			func(t *testing.T, ev Evento) {
				if ev.Bandera != "DOUBLE YELLOW" || ev.Categoria != "Flag" {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"control con flag null", "control", map[string]any{"category": "SafetyCar", "flag": nil, "message": "SAFETY CAR DEPLOYED"}, true,
			func(t *testing.T, ev Evento) {
				if ev.Bandera != "" || ev.Mensaje != "SAFETY CAR DEPLOYED" {
					t.Errorf("ev = %+v", ev)
				}
			}},
		{"vuelta con lap_number decimal", "vuelta", map[string]any{"driver_number": 1.0, "lap_number": 1.5}, false, nil},
		{"clima se ignora", "clima", map[string]any{"air_temperature": 25.0}, false, nil},
		{"ubicacion se ignora", "ubicacion", map[string]any{"driver_number": 1.0, "x": 1.0}, false, nil},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			ev, ok := Normalizar(c.tipo, 1234, "abc", c.raw)
			if ok != c.ok {
				t.Fatalf("ok = %v, se esperaba %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if ev.T != 1234 || ev.ID != "abc" || string(ev.Tipo) != c.tipo {
				t.Errorf("T/ID/Tipo = %d/%s/%s", ev.T, ev.ID, ev.Tipo)
			}
			c.check(t, ev)
		})
	}
}
