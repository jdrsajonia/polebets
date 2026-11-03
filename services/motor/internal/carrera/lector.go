// Package carrera implementa HU2-14: posiciones en vivo leyendo la carrera
// que reproduce el Ingestor (HU2-16/HU2-19) en MongoDB.
//
//	Ingestor (unico que escribe en Mongo, RNF-18)
//	  simulaciones     {_id, carrera_id, estado, t_carrera_ms, vuelta_actual, total_vueltas}
//	  carreras         {carrera_id, pilotos[{numero, codigo, nombre, equipo, color}]}
//	  eventos_carrera  {carrera_id, tipo:"posicion", t_carrera_ms, datos.raw{driver_number, position}}
//	        │ el Motor solo lee
//	        ▼
//	Lector (cada Intervalo) ──► Tablero de HU-05 ──► WebSocket "posiciones"
//
// En cada paso toma la simulacion mas reciente (en_curso, finalizada o
// fallida) y lee los eventos de posicion con t_carrera_ms en
// (cursor, simulacion.t_carrera_ms].
// El Ingestor escribe simulacion.t_carrera_ms DESPUES de marcar los eventos
// de ese tramo, asi que todo lo que este por debajo ya salio: no se pierde un
// evento a medias. Si el Motor se reinicia a mitad de carrera, la primera
// lectura trae todo hasta el instante actual y el tablero queda al dia.
//
// Varios cambios del mismo paso se aplican juntos y se difunde UNA foto, que
// tambien lleva la vuelta actual y el total ("Vuelta X/Y", RF-19). Si solo
// cambia la vuelta, igual se difunde.
package carrera

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// Simulacion es la reproduccion de una carrera (coleccion simulaciones).
type Simulacion struct {
	ID         string
	CarreraID  string
	Estado     string // programada | en_curso | finalizada | fallida
	TCarreraMs int64  // reloj de carrera ya emitido; math.MinInt64 si aun no arranca
	Vuelta     Vuelta
}

// Vuelta es la vuelta actual y el total de la carrera (0 = desconocido).
type Vuelta struct {
	Actual int
	Total  int
}

// Piloto son los datos fijos de un carro (carreras.pilotos).
type Piloto struct {
	Numero int
	Codigo string
	Nombre string
	Equipo string
	Color  string
}

// CambioPosicion es un evento "posicion" de OpenF1.
type CambioPosicion struct {
	TCarreraMs int64
	Numero     int
	Posicion   int
}

// Fuente es lo que el lector necesita de Mongo (implementacion en mongo.go).
type Fuente interface {
	// SimulacionActual devuelve la simulacion mas reciente en_curso,
	// finalizada o fallida (asi el tablero no se queda en una anterior si la
	// ultima fallo); ok=false si no hay ninguna.
	SimulacionActual(ctx context.Context) (Simulacion, bool, error)
	Pilotos(ctx context.Context, carreraID string) ([]Piloto, error)
	// Posiciones devuelve los cambios con desde < t_carrera_ms <= hasta,
	// ordenados por t_carrera_ms.
	Posiciones(ctx context.Context, carreraID string, desde, hasta int64) ([]CambioPosicion, error)
}

// Tablero es el tablero + difusor de HU-05 (adaptado en main.go).
type Tablero interface {
	Iniciar(carreraID string, pilotos []Piloto)
	// Aplicar actualiza varias posiciones (numero -> posicion) y la vuelta, y
	// difunde una foto. cambios puede venir vacio si solo cambio la vuelta.
	Aplicar(cambios map[int]int, vuelta Vuelta)
	// Reenviar difunde otra vez la foto actual, sin cambios.
	Reenviar()
}

// reenvioFoto: mientras haya una simulacion en_curso, la foto actual se
// reenvia con esta frecuencia de hora real aunque no haya cambios, para que
// quien entra o recarga la pagina vea la tabla sin esperar al siguiente
// cambio de posicion (GET /posiciones no llega por el Gateway). La hora real
// solo decide cuando reenviar.
const reenvioFoto = 5 * time.Second

type Lector struct {
	Fuente    Fuente
	Tablero   Tablero
	Intervalo time.Duration
	// Ahora es la hora real, solo para espaciar los reenvios. nil = time.Now.
	Ahora func() time.Time

	simID       string
	cursor      int64     // ultimo t_carrera_ms aplicado de simID
	vuelta      Vuelta    // ultima vuelta difundida de simID
	ultimoEnvio time.Time // ultima foto difundida (Iniciar, Aplicar o Reenviar)
}

func NuevoLector(f Fuente, t Tablero, intervalo time.Duration) *Lector {
	return &Lector{Fuente: f, Tablero: t, Intervalo: intervalo}
}

// Run lee cada Intervalo hasta que se cancele el contexto.
func (l *Lector) Run(ctx context.Context) {
	ticker := time.NewTicker(l.Intervalo)
	defer ticker.Stop()
	for {
		if err := l.Paso(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-14: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Paso hace una lectura y, si toca, reenvia la foto. Si falla no avanza el
// cursor: se reintenta luego.
func (l *Lector) Paso(ctx context.Context) error {
	sim, ok, err := l.paso(ctx)
	if err == nil && ok {
		l.reenviar(sim)
	}
	return err
}

// reenviar difunde la foto actual si la simulacion seguida esta en_curso y
// pasaron reenvioFoto desde la ultima foto.
func (l *Lector) reenviar(sim Simulacion) {
	if sim.Estado != "en_curso" || sim.ID != l.simID {
		return
	}
	ahora := l.ahora()
	if ahora.Sub(l.ultimoEnvio) < reenvioFoto {
		return
	}
	l.Tablero.Reenviar()
	l.ultimoEnvio = ahora
}

func (l *Lector) ahora() time.Time {
	if l.Ahora != nil {
		return l.Ahora()
	}
	return time.Now()
}

func (l *Lector) paso(ctx context.Context) (Simulacion, bool, error) {
	sim, ok, err := l.Fuente.SimulacionActual(ctx)
	if err != nil {
		return sim, false, fmt.Errorf("leer simulacion: %w", err)
	}
	if !ok {
		return sim, false, nil // no hay carrera: el tablero conserva lo que tenia
	}
	if sim.ID != l.simID {
		pilotos, err := l.Fuente.Pilotos(ctx, sim.CarreraID)
		if err != nil {
			return sim, false, fmt.Errorf("leer pilotos de %s: %w", sim.CarreraID, err)
		}
		l.Tablero.Iniciar(sim.CarreraID, pilotos)
		l.ultimoEnvio = l.ahora()
		l.simID = sim.ID
		l.cursor = math.MinInt64 // incluye la parrilla (eventos antes de la largada)
		l.vuelta = Vuelta{}
		log.Printf("HU2-14: siguiendo la simulacion %s (carrera %s, %d pilotos)", sim.ID, sim.CarreraID, len(pilotos))
	}
	if sim.TCarreraMs <= l.cursor {
		return sim, true, nil // el reloj no avanzo
	}
	cambios, err := l.Fuente.Posiciones(ctx, sim.CarreraID, l.cursor, sim.TCarreraMs)
	if err != nil {
		return sim, false, fmt.Errorf("leer posiciones: %w", err)
	}
	ultimas := make(map[int]int, len(cambios))
	for _, c := range cambios { // en orden: gana el mas reciente de cada carro
		if c.Numero > 0 && c.Posicion > 0 {
			ultimas[c.Numero] = c.Posicion
		}
	}
	if len(ultimas) > 0 || sim.Vuelta != l.vuelta {
		l.Tablero.Aplicar(ultimas, sim.Vuelta)
		l.vuelta = sim.Vuelta
		l.ultimoEnvio = l.ahora()
	}
	l.cursor = sim.TCarreraMs
	return sim, true, nil
}
