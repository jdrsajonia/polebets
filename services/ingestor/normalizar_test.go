package main

import (
	"testing"
	"time"
)

var largadaPrueba = time.Date(2023, 9, 17, 12, 0, 0, 0, time.UTC)

func TestTCarreraMsDesdeLaLargada(t *testing.T) {
	casos := []struct {
		fecha string
		want  int64
	}{
		{"2023-09-17T12:00:00+00:00", 0},
		{"2023-09-17T12:03:44.658000+00:00", 224658},
		{"2023-09-17T11:01:04.137000+00:00", -3535863}, // parrilla, antes de la largada
		{"2023-09-17T20:03:44.658+08:00", 224658},      // otra zona horaria
	}
	for _, c := range casos {
		f, ok := parsearFecha(c.fecha)
		if !ok {
			t.Fatalf("no parseó %q", c.fecha)
		}
		if got := tCarreraMs(f, largadaPrueba); got != c.want {
			t.Errorf("%s: t_carrera_ms = %d, want %d", c.fecha, got, c.want)
		}
	}
	for _, malo := range []any{nil, "", "ayer", 12.5} {
		if _, ok := parsearFecha(malo); ok {
			t.Errorf("parsearFecha(%v) debería fallar", malo)
		}
	}
}

func TestTiemposPorVueltaUsaElLiderYVuelta1SinFecha(t *testing.T) {
	laps := []Registro{
		{"lap_number": 1.0, "date_start": nil},                         // vuelta 1 sin fecha → 0
		{"lap_number": 2.0, "date_start": "2023-09-17T12:05:40+00:00"}, // otro piloto, más tarde
		{"lap_number": 2.0, "date_start": "2023-09-17T12:05:35+00:00"}, // el líder
		{"lap_number": 3.0, "date_start": nil},                         // sin fecha y no es la 1 → se salta
		{"lap_number": 4.0, "date_start": "2023-09-17T12:09:10+00:00"},
	}
	v := tiemposPorVuelta(laps, largadaPrueba)
	if v[1] != 0 {
		t.Errorf("vuelta 1 = %d, want 0", v[1])
	}
	if v[2] != 335000 {
		t.Errorf("vuelta 2 = %d, want 335000 (el mínimo, la del líder)", v[2])
	}
	if _, ok := v[3]; ok {
		t.Error("la vuelta 3 sin fecha no debería quedar")
	}
}

func TestTotalVueltasIgnoraLaDeEnfriamiento(t *testing.T) {
	laps := []Registro{
		{"lap_number": 1.0, "lap_duration": nil}, // vuelta 1 sin tiempo (largada)
		{"lap_number": 61.0, "lap_duration": 99.3},
		{"lap_number": 62.0, "lap_duration": 98.2},
		{"lap_number": 62.0, "lap_duration": nil}, // un piloto que abandonó
		{"lap_number": 63.0, "lap_duration": nil}, // enfriamiento tras la bandera
	}
	if got := totalVueltas(laps); got != 62 {
		t.Errorf("total_vueltas = %d, want 62", got)
	}
}

func TestTiempoStint(t *testing.T) {
	vueltas := map[int]int64{2: 335000, 20: 2400000}
	casos := []struct {
		stint Registro
		want  int64
		ok    bool
	}{
		{Registro{"lap_start": 1.0}, 0, true}, // vuelta 1 aunque laps no la traiga
		{Registro{"lap_start": 20.0}, 2400000, true},
		{Registro{"lap_start": 33.0}, 0, false},
		{Registro{"lap_start": nil}, 0, false},
	}
	for _, c := range casos {
		got, ok := tiempoStint(c.stint, vueltas)
		if got != c.want || ok != c.ok {
			t.Errorf("tiempoStint(%v) = (%d, %v), want (%d, %v)", c.stint, got, ok, c.want, c.ok)
		}
	}
}

func TestVentanasCubrenElRangoSinHuecos(t *testing.T) {
	desde := largadaPrueba
	hasta := desde.Add(25 * time.Minute)
	v := ventanas(desde, hasta, 10*time.Minute)
	if len(v) != 3 {
		t.Fatalf("len = %d, want 3", len(v))
	}
	if !v[0].Desde.Equal(desde) || !v[2].Hasta.Equal(hasta) {
		t.Errorf("no cubre el rango: %v", v)
	}
	for i := 1; i < len(v); i++ {
		if !v[i].Desde.Equal(v[i-1].Hasta) {
			t.Errorf("hueco entre %v y %v", v[i-1], v[i])
		}
	}
	if len(ventanas(desde, desde, time.Minute)) != 0 {
		t.Error("rango vacío debería dar 0 ventanas")
	}
}

func TestArmarPilotosConNulosYRepetidos(t *testing.T) {
	drivers := []Registro{
		{"driver_number": 1.0, "name_acronym": "VER", "full_name": "Max VERSTAPPEN", "team_name": "Red Bull Racing", "team_colour": "3671C6"},
		{"driver_number": 1.0, "name_acronym": "VER"},
		{"driver_number": 55.0, "name_acronym": "SAI", "full_name": nil, "team_name": nil, "team_colour": nil},
		{"driver_number": nil},
	}
	pilotos, numeros := armarPilotos(drivers)
	if len(pilotos) != 2 || len(numeros) != 2 || numeros[1] != 55 {
		t.Fatalf("pilotos = %v, numeros = %v", pilotos, numeros)
	}
	sai := pilotos[1]
	for _, campo := range []string{"nombre", "equipo", "color"} {
		if v, ok := sai[campo].(string); !ok || v != "" {
			t.Errorf("%s = %#v, want \"\"", campo, sai[campo])
		}
	}
	if sai["numero"] != int32(55) {
		t.Errorf("numero = %#v, want int32(55)", sai["numero"])
	}
}

func TestNombreCarrera(t *testing.T) {
	s := Registro{"circuit_short_name": "Singapore", "year": 2023.0, "session_name": "Race"}
	if got := nombreCarrera(s); got != "Singapore 2023 - Race" {
		t.Errorf("nombre = %q", got)
	}
}
