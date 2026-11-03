// Orquestador - Prototipo 2 (ArquiSoft), T2-02.
//
// main solo arma las piezas y arranca el servidor. Cada modulo vive en su
// propio paquete bajo internal/ y registra sus rutas con Register(mux, ...),
// igual que en el motor. El contrato de las rutas esta en
// documentation/orquestador-api.md.
//
//   - GET /health : healthcheck (sin sesion; no consulta a otros servicios)
//   - GET /billetera, POST /billetera/compras : compra de tokens (HU2-04)
//   - POST /eventos/{evento_id}/inscripcion : inscripcion al torneo (HU2-06)
//   - POST /registro : registro orquestado, sin sesion (HU2-01)
//   - GET /pagina-usuario : datos de la pagina SSR del jugador (HU2-03)
//   - POST /apuestas : apuesta individual o de torneo (HU2-09)
//   - GET /mercados/{mercado_id}/mi-apuesta : la apuesta del jugador (HU2-13)
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

	"github.com/arquisoft/orquestador/internal/apuesta"
	"github.com/arquisoft/orquestador/internal/billetera"
	"github.com/arquisoft/orquestador/internal/config"
	"github.com/arquisoft/orquestador/internal/eventos"
	"github.com/arquisoft/orquestador/internal/health"
	"github.com/arquisoft/orquestador/internal/inscripcion"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/miapuesta"
	"github.com/arquisoft/orquestador/internal/pagina"
	"github.com/arquisoft/orquestador/internal/registro"
	"github.com/arquisoft/orquestador/internal/traza"
)

func main() {
	cfg, err := config.Cargar()
	if err != nil {
		log.Fatalf("configuracion invalida: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pagos := interno.Nuevo(cfg.PagosBaseURL, cfg.InternalAPIToken)
	usuarios := interno.Nuevo(cfg.UsuariosBaseURL, cfg.InternalAPIToken)
	motor := interno.Nuevo(cfg.MotorBaseURL, cfg.InternalAPIToken)

	mux := http.NewServeMux()
	health.Register(mux)
	billetera.Register(mux, pagos)                                                         // HU2-04: GET /billetera, POST /billetera/compras
	inscripcion.Register(mux, pagos)                                                       // HU2-06: POST /eventos/{evento_id}/inscripcion
	eventos.Register(mux, pagos)                                                           // HU2-05: GET /eventos, GET /eventos/{evento_id}
	registro.Register(mux, pagos, usuarios)                                                // HU2-01: POST /registro (publica)
	pagina.Register(mux, pagina.Servicios{Usuarios: usuarios, Pagos: pagos, Motor: motor}) // HU2-03: GET /pagina-usuario
	apuesta.Register(mux, motor)                                                           // HU2-09: POST /apuestas
	miapuesta.Register(mux, pagos)                                                         // HU2-13: GET /mercados/{mercado_id}/mi-apuesta

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
