// Ingestor — STUB base (T2-04).
//
// Esqueleto mínimo del Ingestor (HU2-16, HU2-19, Iván). Solo responde /health
// para que el contenedor arranque y todo el stack levante con un solo comando.
//
// Qué va aquí cuando se implemente el servicio real:
//   - Descargar carreras de OpenF1 por tramos (posiciones, coordenadas x/y,
//     vueltas, pits, control de carrera) con un comando del Ingestor (HU2-16).
//   - Recibir por el Broker (RabbitMQ) la orden de iniciar simulación (HU2-19).
//   - Reproducir la carrera según el TIEMPO DE CARRERA y escribir cada dato en
//     MongoDB. Es el ÚNICO componente que escribe en MongoDB (RNF-18).
//
// Puerto interno: 8002 (no se publica al host).
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	puerto := os.Getenv("HTTP_PORT")
	if puerto == "" {
		puerto = "8002"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"estado":"ok","servicio":"ingestor","nota":"stub T2-04"}`))
	})

	log.Printf("ingestor (stub) escuchando en :%s", puerto)
	if err := http.ListenAndServe(":"+puerto, mux); err != nil {
		log.Fatalf("servidor HTTP terminó: %v", err)
	}
}
