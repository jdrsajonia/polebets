// Package visor implementa HU2-15 del lado del Motor: difunde por WebSocket
// lo que necesita el visor de pista (C# / Blazor) para dibujar la carrera.
//
//	Ingestor (unico que escribe en Mongo, RNF-18)
//	  simulaciones     {_id, carrera_id, estado, t_carrera_ms}
//	  eventos_carrera  {tipo:"vuelta"} y {tipo:"ubicacion", datos.raw{driver_number, x, y}}
//	        │ el Motor solo lee
//	        ▼
//	Lector (cada Intervalo) ──► "pista"        trazado de una vuelta completa
//	                        └─► "coordenadas"  x/y de cada auto en el ultimo tramo
//
// La pista sale de la vuelta mas rapida de la carrera (limpia, sin pits): las
// coordenadas de ese piloto en esa vuelta dibujan el circuito. Se difunde al
// tomar la simulacion y se reenvia cada reenvioPista mientras este programada
// o en curso, para que un visor que se abre tarde la reciba sin pedirla
// (GET al Motor no llega por el Gateway).
//
// Las coordenadas se leen igual que las posiciones de HU2-14: los eventos con
// t_carrera_ms en (cursor, simulacion.t_carrera_ms]. El Ingestor escribe ese
// reloj DESPUES de marcar el tramo, asi que nunca se lee uno a medias. Cada
// paso con datos nuevos manda UN mensaje con todas las muestras del tramo, cada
// una con su t_carrera_ms: el visor interpola entre ellas (RNF-13).
package visor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"
)

// Tipos de mensaje WebSocket (mismos valores que ws.TipoPista y
// ws.TipoCoordenadas; este paquete no importa ws para probarse solo).
const (
	TipoPista       = "pista"
	TipoCoordenadas = "coordenadas"
)

const (
	// reenvioPista: cada cuanto se repite la pista mientras haya simulacion.
	reenvioPista = 5 * time.Second
	// tramoMaxMs: si el cursor se atraso mas que esto (Motor recien arrancado
	// o Mongo lento), se salta al ultimo tramoInicialMs: al visor solo le
	// sirve el presente, y asi una lectura nunca trae media carrera.
	tramoMaxMs     = 10_000
	tramoInicialMs = 2_000
)

// ErrSinTrazado: la carrera no tiene una vuelta con coordenadas para dibujar.
var ErrSinTrazado = errors.New("la carrera no tiene una vuelta completa con coordenadas")

// Simulacion es la reproduccion programada o en curso (coleccion simulaciones).
type Simulacion struct {
	ID         string
	CarreraID  string
	Estado     string // programada | en_curso
	TCarreraMs int64  // reloj de carrera ya emitido; math.MinInt64 si aun no arranca
}

// Punto es una coordenada x/y de OpenF1; en JSON va como [x, y].
type Punto [2]float64

// Trazado es la vuelta que dibuja la pista.
type Trazado struct {
	Piloto int
	Vuelta int
	Puntos []Punto
}

// Muestra es la posicion de un auto en un instante de la carrera.
type Muestra struct {
	Numero int     `json:"numero"`
	T      int64   `json:"t"` // t_carrera_ms de la muestra
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// MensajePista es el payload de "pista".
type MensajePista struct {
	SimulacionID string    `json:"simulacion_id"`
	CarreraID    string    `json:"carrera_id"`
	Estado       string    `json:"estado"`
	Piloto       int       `json:"piloto_referencia"`
	Vuelta       int       `json:"vuelta_referencia"`
	Puntos       []Punto   `json:"puntos"`
	EmitidoEn    time.Time `json:"emitido_en"`
}

// MensajeCoordenadas es el payload de "coordenadas".
type MensajeCoordenadas struct {
	SimulacionID string    `json:"simulacion_id"`
	CarreraID    string    `json:"carrera_id"`
	TCarreraMs   int64     `json:"t_carrera_ms"` // hasta donde llega este tramo
	Muestras     []Muestra `json:"muestras"`
	EmitidoEn    time.Time `json:"emitido_en"` // para medir la latencia (RNF-13)
}

// Fuente es lo que el lector necesita de Mongo (implementacion en mongo.go).
type Fuente interface {
	// SimulacionVigente devuelve la simulacion programada o en curso; ok=false si no hay.
	SimulacionVigente(ctx context.Context) (Simulacion, bool, error)
	// Trazado devuelve la vuelta que dibuja la pista, o ErrSinTrazado.
	Trazado(ctx context.Context, carreraID string) (Trazado, error)
	// Muestras devuelve las coordenadas con desde < t_carrera_ms <= hasta, ordenadas por t.
	Muestras(ctx context.Context, carreraID string, desde, hasta int64) ([]Muestra, error)
}

// Difusor envia un mensaje a todos los conectados (el hub, adaptado en main.go).
type Difusor interface {
	Difundir(tipo string, payload any) error
}

// Lector sigue la simulacion vigente y difunde la pista y las coordenadas.
type Lector struct {
	Fuente    Fuente
	Difusor   Difusor
	Intervalo time.Duration
	// Ahora es la hora real (espacia los reenvios y marca emitido_en). nil = time.Now.
	Ahora func() time.Time

	simID       string
	trazado     *Trazado // nil = aun no se pudo leer; vacio = la carrera no tiene
	cursor      int64    // ultimo t_carrera_ms difundido de simID
	ultimaPista time.Time
}

// NuevoLector arma un lector que lee cada intervalo.
func NuevoLector(f Fuente, d Difusor, intervalo time.Duration) *Lector {
	return &Lector{Fuente: f, Difusor: d, Intervalo: intervalo}
}

// Run lee cada Intervalo hasta que se cancele el contexto.
func (l *Lector) Run(ctx context.Context) {
	ticker := time.NewTicker(l.Intervalo)
	defer ticker.Stop()
	for {
		if err := l.Paso(ctx); err != nil && ctx.Err() == nil {
			log.Printf("HU2-15: %v", err)
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
	sim, ok, err := l.Fuente.SimulacionVigente(ctx)
	if err != nil {
		return fmt.Errorf("leer simulacion: %w", err)
	}
	if !ok {
		l.simID = "" // sin carrera: el visor conserva lo ultimo que dibujo
		return nil
	}
	if sim.ID != l.simID {
		l.simID, l.trazado, l.cursor, l.ultimaPista = sim.ID, nil, math.MinInt64, time.Time{}
		log.Printf("HU2-15: siguiendo la simulacion %s (carrera %s, %s)", sim.ID, sim.CarreraID, sim.Estado)
	}
	if err := l.pista(ctx, sim); err != nil {
		return err
	}
	if sim.Estado != "en_curso" || sim.TCarreraMs == math.MinInt64 {
		return nil // programada: hay pista pero aun no hay autos
	}
	return l.coordenadas(ctx, sim)
}

// pista lee el trazado una vez por simulacion y lo (re)envia cada reenvioPista.
func (l *Lector) pista(ctx context.Context, sim Simulacion) error {
	if l.trazado == nil {
		t, err := l.Fuente.Trazado(ctx, sim.CarreraID)
		switch {
		case errors.Is(err, ErrSinTrazado):
			log.Printf("HU2-15: carrera %s sin trazado: el visor no tendra pista", sim.CarreraID)
			t = Trazado{}
		case err != nil:
			return fmt.Errorf("leer trazado de %s: %w", sim.CarreraID, err)
		}
		l.trazado = &t
	}
	ahora := l.ahora()
	if len(l.trazado.Puntos) == 0 || (!l.ultimaPista.IsZero() && ahora.Sub(l.ultimaPista) < reenvioPista) {
		return nil
	}
	l.ultimaPista = ahora
	l.difundir(TipoPista, MensajePista{
		SimulacionID: sim.ID,
		CarreraID:    sim.CarreraID,
		Estado:       sim.Estado,
		Piloto:       l.trazado.Piloto,
		Vuelta:       l.trazado.Vuelta,
		Puntos:       l.trazado.Puntos,
		EmitidoEn:    ahora.UTC(),
	})
	return nil
}

// coordenadas difunde las muestras del tramo (cursor, reloj de la simulacion].
func (l *Lector) coordenadas(ctx context.Context, sim Simulacion) error {
	desde := l.cursor
	if desde == math.MinInt64 || sim.TCarreraMs-desde > tramoMaxMs {
		desde = sim.TCarreraMs - tramoInicialMs
	}
	if sim.TCarreraMs <= desde {
		return nil // el reloj no avanzo
	}
	muestras, err := l.Fuente.Muestras(ctx, sim.CarreraID, desde, sim.TCarreraMs)
	if err != nil {
		return fmt.Errorf("leer coordenadas: %w", err)
	}
	if len(muestras) > 0 {
		l.difundir(TipoCoordenadas, MensajeCoordenadas{
			SimulacionID: sim.ID,
			CarreraID:    sim.CarreraID,
			TCarreraMs:   sim.TCarreraMs,
			Muestras:     muestras,
			EmitidoEn:    l.ahora().UTC(),
		})
	}
	l.cursor = sim.TCarreraMs
	return nil
}

// difundir envia y, si falla, solo lo registra: el siguiente paso sigue.
func (l *Lector) difundir(tipo string, payload any) {
	if err := l.Difusor.Difundir(tipo, payload); err != nil {
		log.Printf("HU2-15: no se pudo difundir %s: %v", tipo, err)
	}
}

// ahora es la hora real (inyectable en las pruebas).
func (l *Lector) ahora() time.Time {
	if l.Ahora != nil {
		return l.Ahora()
	}
	return time.Now()
}
