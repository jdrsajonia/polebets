package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/arquisoft/motor/internal/auth"
	"github.com/arquisoft/motor/internal/mongodb"
)

// Register monta las rutas de ingesta (HU-04).
//
//	POST /ingest/openf1  → trae datos reales de OpenF1 y los guarda en Mongo
//	POST /ingest/replay  → reproduce una carrera ya guardada
//
// Ambas rutas exigen rol administrador. La reproduccion no difunde nada por
// si misma: avisa a observador (en main, el difusor de posiciones de HU-05),
// que arma la foto completa de posiciones y la envia por WebSocket.
func Register(
	mux *http.ServeMux,
	db *mongodb.Conexion,
	a *auth.Autenticador,
	observador ObservadorCarrera,
) {
	svc := NuevoServicio(db.DB)
	activa := &reproduccionActiva{}

	soloAdmin := func(h http.Handler) http.Handler {
		return a.ExigirRol(auth.RolAdmin, h)
	}

	// ---- POST /ingest/openf1 ---------------------------------------------
	mux.Handle("POST /ingest/openf1", soloAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CarreraID  string `json:"carrera_id"`
			SessionKey int    `json:"session_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "json invalido: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.CarreraID == "" || body.SessionKey == 0 {
			http.Error(w, "carrera_id y session_key son obligatorios", http.StatusBadRequest)
			return
		}

		n, err := svc.IngestaOpenF1(r.Context(), body.CarreraID, body.SessionKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"insertados": n})
	})))

	// ---- POST /ingest/replay ---------------------------------------------
	//
	// Cuerpo: {"carrera_id": "...", "factor": 1, "offset_segundos": 1200}
	//   - factor: velocidad (1 = tiempo real; es el recomendado para los
	//     mercados Si/No, ver Replay).
	//   - offset_segundos (opcional): desde que segundo de la carrera
	//     reproducir, contado desde la primera posicion. Si no viene, se elige
	//     al azar despues de las primeras vueltas (OffsetAleatorio), para no
	//     empezar con la parrilla de salida. Sirve fijarlo para repetir una
	//     prueba.
	mux.Handle("POST /ingest/replay", soloAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CarreraID      string   `json:"carrera_id"`
			Factor         float64  `json:"factor"`
			OffsetSegundos *float64 `json:"offset_segundos"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "json invalido: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.CarreraID == "" {
			http.Error(w, "carrera_id es obligatorio", http.StatusBadRequest)
			return
		}
		if body.Factor <= 0 {
			body.Factor = 1
		}

		inicio, fin, err := svc.RangoCarrera(r.Context(), body.CarreraID)
		if errors.Is(err, ErrSinPosiciones) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "no se pudo leer la carrera: "+err.Error(), http.StatusInternalServerError)
			return
		}
		duracion := fin.Sub(inicio)
		var offset time.Duration
		if body.OffsetSegundos != nil {
			offset = time.Duration(*body.OffsetSegundos * float64(time.Second))
			if offset < 0 || offset > duracion {
				http.Error(w, fmt.Sprintf("offset_segundos debe estar entre 0 y %.0f", duracion.Seconds()), http.StatusBadRequest)
				return
			}
		} else {
			offset = OffsetAleatorio(duracion, rand.Int64N)
		}
		desde := inicio.Add(offset)

		// HU-05: donde iba cada piloto en desde, para que la tabla arranque
		// completa aunque la reproduccion empiece a mitad de carrera.
		pilotos, posiciones, err := svc.EstadoInicial(r.Context(), body.CarreraID, desde)
		if err != nil {
			http.Error(w, "no se pudo leer el estado inicial: "+err.Error(), http.StatusInternalServerError)
			return
		}
		carreraID, factor := body.CarreraID, body.Factor

		// Una sola reproduccion a la vez: si ya habia una, se cancela y se
		// espera a que termine antes de iniciar el tablero de la nueva.
		activa.lanzar(30*time.Minute,
			func() {
				observador.Iniciar(carreraID, pilotos, posiciones)
				log.Printf("replay %s: desde %s (offset %s de %s), factor %g",
					carreraID, desde.Format(time.RFC3339), offset.Round(time.Second), duracion.Round(time.Second), factor)
			},
			func(ctx context.Context) {
				eventos := make(chan EventoCarrera, 64)
				go func() {
					defer close(eventos)
					err := svc.Replay(ctx, carreraID, desde, factor, eventos)
					switch {
					case errors.Is(err, context.Canceled):
						log.Printf("replay %s cancelado (se lanzo otro)", carreraID)
					case err != nil:
						log.Printf("replay %s termino con error: %v", carreraID, err)
					}
				}()
				consumir(ctx, eventos, observador)
				log.Printf("replay %s terminado", carreraID)
			},
		)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"estado":          "reproduciendo",
			"carrera_id":      body.CarreraID,
			"factor":          body.Factor,
			"offset_segundos": offset.Seconds(),
			"desde":           desde, // instante (historico) de la carrera
		})
	})))
}

// consumir pasa al observador los eventos de posicion de la reproduccion; los
// demas tipos solo quedan marcados con emitido_en por Replay. Si ctx se
// cancela, vacia el canal sin aplicar nada: los eventos que quedaron en el
// buffer son de la reproduccion vieja y no deben tocar el tablero de la nueva.
// Termina cuando Replay cierra el canal, es decir, cuando ya paro del todo
// (latido incluido).
func consumir(ctx context.Context, eventos <-chan EventoCarrera, observador ObservadorCarrera) {
	for ev := range eventos {
		if ctx.Err() != nil {
			continue
		}
		if numero, posicion, ok := posicionDe(ev); ok {
			observador.Actualizar(numero, posicion)
		}
	}
}

// reproduccionActiva guarda la reproduccion en curso. Sin esto, lanzar
// POST /ingest/replay dos veces dejaba dos goroutines emitiendo a la vez
// (eventos duplicados y la tabla de posiciones saltando entre las dos).
type reproduccionActiva struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// lanzar cancela la reproduccion anterior y espera a que termine; luego
// llama a antes (iniciar el tablero) y arranca correr en una goroutine con
// su propio contexto, que vence a los timeout. El mutex se mantiene durante
// todo el cambio, asi dos relanzamientos simultaneos no se cruzan.
//
// El latido de la vieja no pisa a la nueva: Replay lo detiene al salir y sus
// escrituras filtran por su propio iniciada_en (ver iniciarLatido).
func (a *reproduccionActiva) lanzar(timeout time.Duration, antes func(), correr func(ctx context.Context)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		<-a.done
	}
	antes()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	done := make(chan struct{})
	a.cancel, a.done = cancel, done
	go func() {
		defer close(done)
		defer cancel()
		correr(ctx)
	}()
}
