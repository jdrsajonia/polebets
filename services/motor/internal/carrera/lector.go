// Package carrera implementa HU2-14: posiciones en vivo leyendo la carrera
// que reproduce el Ingestor (HU2-16/HU2-19) en MongoDB.
//
//	Ingestor (unico que escribe en Mongo, RNF-18)
//	  simulaciones     {_id, carrera_id, estado, t_carrera_ms}
//	  carreras         {carrera_id, pilotos[{numero, codigo, nombre, equipo, color}]}
//	  eventos_carrera  {carrera_id, tipo:"posicion", t_carrera_ms, datos.raw{driver_number, position}}
//	        │ el Motor solo lee
//	        ▼
//	Lector (cada Intervalo) ──► Tablero de HU-05 ──► WebSocket "posiciones"
//
// En cada paso toma la simulacion mas reciente (en_curso o finalizada) y lee
// los eventos de posicion con t_carrera_ms en (cursor, simulacion.t_carrera_ms].
// El Ingestor escribe simulacion.t_carrera_ms DESPUES de marcar los eventos
// de ese tramo, asi que todo lo que este por debajo ya salio: no se pierde un
// evento a medias. Si el Motor se reinicia a mitad de carrera, la primera
// lectura trae todo hasta el instante actual y el tablero queda al dia.
//
// Varios cambios del mismo paso se aplican juntos y se difunde UNA foto.
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
	Estado     string // programada | en_curso | finalizada
	TCarreraMs int64  // reloj de carrera ya emitido; math.MinInt64 si aun no arranca
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
	// SimulacionActual devuelve la simulacion mas reciente en_curso o
	// finalizada; ok=false si no hay ninguna.
	SimulacionActual(ctx context.Context) (Simulacion, bool, error)
	Pilotos(ctx context.Context, carreraID string) ([]Piloto, error)
	// Posiciones devuelve los cambios con desde < t_carrera_ms <= hasta,
	// ordenados por t_carrera_ms.
	Posiciones(ctx context.Context, carreraID string, desde, hasta int64) ([]CambioPosicion, error)
}

// Tablero es el tablero + difusor de HU-05 (adaptado en main.go).
type Tablero interface {
	Iniciar(carreraID string, pilotos []Piloto)
	// Aplicar actualiza varias posiciones (numero -> posicion) y difunde una foto.
	Aplicar(cambios map[int]int)
}

type Lector struct {
	Fuente    Fuente
	Tablero   Tablero
	Intervalo time.Duration

	simID  string
	cursor int64 // ultimo t_carrera_ms aplicado de simID
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

// Paso hace una lectura. Si falla no avanza el cursor: se reintenta luego.
func (l *Lector) Paso(ctx context.Context) error {
	sim, ok, err := l.Fuente.SimulacionActual(ctx)
	if err != nil {
		return fmt.Errorf("leer simulacion: %w", err)
	}
	if !ok {
		return nil // no hay carrera: el tablero conserva lo que tenia
	}
	if sim.ID != l.simID {
		pilotos, err := l.Fuente.Pilotos(ctx, sim.CarreraID)
		if err != nil {
			return fmt.Errorf("leer pilotos de %s: %w", sim.CarreraID, err)
		}
		l.Tablero.Iniciar(sim.CarreraID, pilotos)
		l.simID = sim.ID
		l.cursor = math.MinInt64 // incluye la parrilla (eventos antes de la largada)
		log.Printf("HU2-14: siguiendo la simulacion %s (carrera %s, %d pilotos)", sim.ID, sim.CarreraID, len(pilotos))
	}
	if sim.TCarreraMs <= l.cursor {
		return nil // el reloj no avanzo
	}
	cambios, err := l.Fuente.Posiciones(ctx, sim.CarreraID, l.cursor, sim.TCarreraMs)
	if err != nil {
		return fmt.Errorf("leer posiciones: %w", err)
	}
	if len(cambios) > 0 {
		ultimas := make(map[int]int, len(cambios))
		for _, c := range cambios { // en orden: gana el mas reciente de cada carro
			if c.Numero > 0 && c.Posicion > 0 {
				ultimas[c.Numero] = c.Posicion
			}
		}
		if len(ultimas) > 0 {
			l.Tablero.Aplicar(ultimas)
		}
	}
	l.cursor = sim.TCarreraMs
	return nil
}
