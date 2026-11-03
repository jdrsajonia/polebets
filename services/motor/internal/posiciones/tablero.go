// Package posiciones implementa HU-05: la tabla de posiciones en vivo.
package posiciones

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Piloto es una fila de la tabla.
type Piloto struct {
	Posicion    int    `json:"posicion"`
	Numero      int    `json:"numero"`
	Codigo      string `json:"codigo"`
	Nombre      string `json:"nombre"`
	Equipo      string `json:"equipo"`
	ColorEquipo string `json:"color_equipo,omitempty"`
	Diferencia  string `json:"diferencia"`
}

// FotoPosiciones es el estado completo de la carrera en un instante.
type FotoPosiciones struct {
	CarreraID     string    `json:"carrera_id"`
	Vuelta        int       `json:"vuelta,omitempty"`        // vuelta actual (HU2-14); 0 = desconocida
	TotalVueltas  int       `json:"total_vueltas,omitempty"` // total de la carrera (HU2-14)
	ActualizadoEn time.Time `json:"actualizado_en"`
	EmitidoEn     time.Time `json:"emitido_en,omitzero"`
	Pilotos       []Piloto  `json:"pilotos"`
}

const (
	DiferenciaLider = "Líder"
	SinDiferencia   = "—"
)

// Tablero es la carrera en curso.
type Tablero struct {
	mu         sync.Mutex
	ahora      func() time.Time
	iniciado   bool
	carreraID  string
	pilotos    map[int]PilotoCarrera // datos fijos por numero de carro
	posiciones map[int]int           // posicion actual por numero
	vuelta     int                   // vuelta actual (HU2-14)
	total      int                   // total de vueltas (HU2-14)
	foto       FotoPosiciones        // ultima foto armada
}

func NuevoTablero() *Tablero {
	return &Tablero{ahora: time.Now}
}

// Iniciar arranca una carrera nueva y reemplaza la anterior.
func (t *Tablero) Iniciar(carreraID string, pilotos []PilotoCarrera, posiciones map[int]int) FotoPosiciones {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.iniciado = true
	t.carreraID = carreraID
	t.pilotos = make(map[int]PilotoCarrera, len(pilotos))
	for _, p := range pilotos {
		t.pilotos[p.Numero] = p
	}
	t.vuelta, t.total = 0, 0
	t.posiciones = make(map[int]int, len(posiciones))
	for numero, posicion := range posiciones {
		t.posiciones[numero] = posicion
	}
	t.foto = t.armar()
	return t.foto
}

// Actualizar aplica un evento de posicion y devuelve la foto completa nueva.
func (t *Tablero) Actualizar(numero, posicion int) (FotoPosiciones, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.iniciado {
		return FotoPosiciones{}, false
	}
	t.posiciones[numero] = posicion
	t.foto = t.armar()
	return t.foto, true
}

// ActualizarVarias aplica varios eventos de posicion (numero -> posicion) y la
// vuelta, y arma una sola foto (HU2-14: el lector trae los cambios por tramos).
func (t *Tablero) ActualizarVarias(cambios map[int]int, vuelta, total int) (FotoPosiciones, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.iniciado {
		return FotoPosiciones{}, false
	}
	for numero, posicion := range cambios {
		t.posiciones[numero] = posicion
	}
	t.vuelta, t.total = vuelta, total
	t.foto = t.armar()
	return t.foto, true
}

// Foto devuelve la ultima foto; false si no hay carrera.
func (t *Tablero) Foto() (FotoPosiciones, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.foto, t.iniciado
}

func (t *Tablero) armar() FotoPosiciones {
	filas := make([]Piloto, 0, len(t.posiciones))
	for numero, posicion := range t.posiciones {
		filas = append(filas, armarPiloto(numero, posicion, t.pilotos[numero]))
	}
	slices.SortFunc(filas, func(a, b Piloto) int {
		return cmp.Or(cmp.Compare(a.Posicion, b.Posicion), cmp.Compare(a.Numero, b.Numero))
	})
	return FotoPosiciones{
		CarreraID:     t.carreraID,
		Vuelta:        t.vuelta,
		TotalVueltas:  t.total,
		ActualizadoEn: t.ahora().UTC(),
		Pilotos:       filas,
	}
}

func armarPiloto(numero, posicion int, datos PilotoCarrera) Piloto {
	p := Piloto{
		Posicion:   posicion,
		Numero:     numero,
		Codigo:     datos.Codigo,
		Nombre:     datos.Nombre,
		Equipo:     datos.Equipo,
		Diferencia: SinDiferencia,
	}
	if p.Codigo == "" {
		p.Codigo = strconv.Itoa(numero)
	}
	if color := strings.TrimPrefix(datos.Color, "#"); color != "" {
		p.ColorEquipo = "#" + color
	}
	if posicion == 1 {
		p.Diferencia = DiferenciaLider
	}
	return p
}
