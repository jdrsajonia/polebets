package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// formatoFiltroFecha es como OpenF1 acepta las fechas en los filtros (UTC, sin zona).
const formatoFiltroFecha = "2006-01-02T15:04:05"

// parsearFecha lee una fecha ISO 8601 de OpenF1 ("2023-09-17T12:03:44.658000+00:00").
func parsearFecha(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// tCarreraMs es el tiempo de carrera de un dato: su fecha menos la largada de
// la sesión. Es negativo para lo que pasó antes de la largada (clima, parrilla).
func tCarreraMs(fecha, largada time.Time) int64 {
	return fecha.Sub(largada).Milliseconds()
}

// entero lee un número de OpenF1 (json lo decodifica como float64).
func entero(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// texto devuelve "" si OpenF1 manda null, para no romper el validador de
// `carreras`, que exige string.
func texto(v any) string {
	s, _ := v.(string)
	return s
}

// tiempoVuelta da el t_carrera_ms en que arranca una vuelta (date_start). En
// carrera la vuelta 1 suele venir sin date_start: arranca con la largada (t = 0).
func tiempoVuelta(lap Registro, largada time.Time) (int64, bool) {
	if fecha, ok := parsearFecha(lap["date_start"]); ok {
		return tCarreraMs(fecha, largada), true
	}
	if n, ok := entero(lap["lap_number"]); ok && n == 1 {
		return 0, true
	}
	return 0, false
}

// tiemposPorVuelta guarda, por número de vuelta, el primer piloto que la
// empezó (el líder). Sirve para fechar los stints, que no traen fecha.
func tiemposPorVuelta(laps []Registro, largada time.Time) map[int]int64 {
	vueltas := make(map[int]int64)
	for _, lap := range laps {
		n, ok := entero(lap["lap_number"])
		if !ok {
			continue
		}
		t, ok := tiempoVuelta(lap, largada)
		if !ok {
			continue
		}
		if previo, existe := vueltas[n]; !existe || t < previo {
			vueltas[n] = t
		}
	}
	return vueltas
}

// tiempoStint fecha un stint con el t_carrera_ms de su vuelta lap_start.
func tiempoStint(stint Registro, vueltas map[int]int64) (int64, bool) {
	vuelta, ok := entero(stint["lap_start"])
	if !ok {
		return 0, false
	}
	if t, ok := vueltas[vuelta]; ok {
		return t, true
	}
	if vuelta == 1 {
		return 0, true
	}
	return 0, false
}

// totalVueltas es la vuelta más alta que algún piloto completó (con
// lap_duration). La vuelta de enfriamiento tras la bandera a cuadros aparece
// en /laps sin duración y no cuenta.
func totalVueltas(laps []Registro) int {
	total := 0
	for _, lap := range laps {
		n, ok := entero(lap["lap_number"])
		if _, completa := lap["lap_duration"].(float64); ok && completa && n > total {
			total = n
		}
	}
	return total
}

// ventana es un tramo [Desde, Hasta) para pedir datos pesados por partes.
type ventana struct {
	Desde, Hasta time.Time
}

// ventanas parte [desde, hasta) en tramos de `paso`; el último puede ser más corto.
func ventanas(desde, hasta time.Time, paso time.Duration) []ventana {
	var res []ventana
	if paso <= 0 {
		return res
	}
	for ini := desde; ini.Before(hasta); ini = ini.Add(paso) {
		fin := ini.Add(paso)
		if fin.After(hasta) {
			fin = hasta
		}
		res = append(res, ventana{Desde: ini, Hasta: fin})
	}
	return res
}

// armarPilotos convierte /drivers al formato de `carreras.pilotos`, sin
// repetidos y con "" en los campos que OpenF1 manda en null.
func armarPilotos(drivers []Registro) ([]bson.M, []int) {
	pilotos := make([]bson.M, 0, len(drivers))
	var numeros []int
	vistos := make(map[int]bool)
	for _, d := range drivers {
		num, ok := entero(d["driver_number"])
		if !ok || vistos[num] {
			continue
		}
		vistos[num] = true
		numeros = append(numeros, num)
		pilotos = append(pilotos, bson.M{
			"numero": int32(num),
			"codigo": texto(d["name_acronym"]),
			"nombre": texto(d["full_name"]),
			"equipo": texto(d["team_name"]),
			"color":  texto(d["team_colour"]),
		})
	}
	return pilotos, numeros
}

// nombreCarrera arma un nombre legible: session_name solo dice "Race".
func nombreCarrera(sesion Registro) string {
	lugar := texto(sesion["circuit_short_name"])
	if lugar == "" {
		lugar = texto(sesion["country_name"])
	}
	anio, _ := entero(sesion["year"])
	nombre := strings.TrimSpace(fmt.Sprintf("%s %d", lugar, anio))
	if s := texto(sesion["session_name"]); s != "" {
		nombre += " - " + s
	}
	return nombre
}
