// Package posiciones implementa HU-05: la tabla de posiciones en vivo.
//
//   - Tablero guarda en memoria la carrera que se esta reproduciendo y arma
//     la "foto" completa de posiciones (todos los pilotos, ordenados).
//   - Difusor cumple ingest.ObservadorCarrera: la reproduccion le avisa de
//     cada cambio y el difunde la foto nueva por WebSocket (mensaje
//     "posiciones").
//   - Register monta GET /posiciones, que devuelve la ultima foto.
//
// El contrato de la foto es el del front (services/front/src/api/tipos.ts:
// Piloto y FotoPosiciones).
package posiciones

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arquisoft/motor/internal/ingest"
)

// Piloto es una fila de la tabla.
type Piloto struct {
	Posicion    int    `json:"posicion"`
	Numero      int    `json:"numero"`
	Codigo      string `json:"codigo"`
	Nombre      string `json:"nombre"`
	Equipo      string `json:"equipo"`
	ColorEquipo string `json:"color_equipo,omitempty"` // "#3671C6"; se omite si no hay
	Diferencia  string `json:"diferencia"`
}

// FotoPosiciones es el estado completo de la carrera en un instante. Es el
// payload del mensaje "posiciones" y la respuesta de GET /posiciones.
//
// ActualizadoEn es la hora REAL del servidor al aplicar el cambio, no el
// timestamp historico del evento: el front descarta fotos con actualizado_en
// mas viejo que la que ya tiene, y relanzar la reproduccion desde un punto
// anterior de la carrera dejaria la tabla congelada. No hay "vuelta": OpenF1
// /position no la trae.
type FotoPosiciones struct {
	CarreraID     string    `json:"carrera_id"`
	ActualizadoEn time.Time `json:"actualizado_en"`
	EmitidoEn     time.Time `json:"emitido_en,omitzero"` // lo pone el Difusor al enviar (RNF-04)
	Pilotos       []Piloto  `json:"pilotos"`
}

// Diferencia con el lider: no se ingieren intervalos (OpenF1 /intervals
// queda como mejora), asi que solo se marca al P1.
const (
	DiferenciaLider = "Líder"
	SinDiferencia   = "—"
)

// Tablero es la carrera en curso. Lo escribe la reproduccion y lo lee
// GET /posiciones desde otras goroutines, por eso el mutex.
type Tablero struct {
	mu         sync.Mutex
	ahora      func() time.Time
	iniciado   bool
	carreraID  string
	pilotos    map[int]ingest.PilotoCarrera // datos fijos por numero de carro
	posiciones map[int]int                  // posicion actual por numero
	foto       FotoPosiciones               // ultima foto armada
}

func NuevoTablero() *Tablero {
	return &Tablero{ahora: time.Now}
}

// Iniciar arranca una carrera nueva y reemplaza la anterior. posiciones es
// donde iba cada piloto al empezar la reproduccion (puede estar vacio).
func (t *Tablero) Iniciar(carreraID string, pilotos []ingest.PilotoCarrera, posiciones map[int]int) FotoPosiciones {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.iniciado = true
	t.carreraID = carreraID
	t.pilotos = make(map[int]ingest.PilotoCarrera, len(pilotos))
	for _, p := range pilotos {
		t.pilotos[p.Numero] = p
	}
	// Copia: el mapa del llamador no debe cambiar el tablero desde afuera.
	t.posiciones = make(map[int]int, len(posiciones))
	for numero, posicion := range posiciones {
		t.posiciones[numero] = posicion
	}
	t.foto = t.armar()
	return t.foto
}

// Actualizar aplica un evento de posicion y devuelve la foto completa nueva.
// ok es false si todavia no hay carrera (no se llamo a Iniciar).
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

// Foto devuelve la ultima foto; false si no hay carrera. Al terminar la
// reproduccion la foto se queda como estaba.
func (t *Tablero) Foto() (FotoPosiciones, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.foto, t.iniciado
}

// armar construye la foto a partir de las posiciones actuales. Se llama con
// el mutex tomado. Cada foto lleva su propio slice, que nadie modifica
// despues: se puede leer o serializar fuera del mutex.
//
// Si dos pilotos tienen un instante la misma posicion (OpenF1 manda los dos
// lados de un adelantamiento en eventos separados), salen los dos con ella,
// desempatados por numero; el siguiente evento lo corrige.
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
		ActualizadoEn: t.ahora().UTC(),
		Pilotos:       filas,
	}
}

// armarPiloto junta la posicion con los datos de lista_pilotos. Un piloto que
// no esta en la lista sale con su numero como codigo y sin nombre.
func armarPiloto(numero, posicion int, datos ingest.PilotoCarrera) Piloto {
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
