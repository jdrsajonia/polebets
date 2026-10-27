// Orquestador - Prototipo 2 (ArquiSoft), T2-02.
//
// main solo arma las piezas y arranca el servidor. Cada modulo vive en su
// propio paquete bajo internal/ y registra sus rutas con Register(mux, ...),
// igual que en el motor. El contrato de las rutas esta en
// documentation/orquestador-api.md.
//
//   - GET /health : healthcheck (sin sesion; no consulta a otros servicios)
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arquisoft/orquestador/internal/config"
	"github.com/arquisoft/orquestador/internal/health"
	"github.com/arquisoft/orquestador/internal/traza"
)

func main() {
	cfg, err := config.Cargar()
	if err != nil {
		log.Fatalf("configuracion invalida: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	health.Register(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           traza.Middleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("apagando el orquestador...")
		ctxApagado, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctxApagado)
	}()

	log.Printf("orquestador escuchando en %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("servidor HTTP termino: %v", err)
	}
}
