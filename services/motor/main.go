// Motor de apuestas - Prototipo 1 (ArquiSoft).
//
// main solo arma las piezas y arranca el servidor. Cada modulo vive en su
// propio paquete bajo internal/ y registra sus rutas con Register(mux, ...),
// para que cada HU se agregue sin editar el codigo de las demas.
//
//   - GET /health : healthcheck (503 si MongoDB no responde)
//   - GET /ws     : WebSocket para datos en vivo (exige la cookie de sesion)
//   - POST /mercados, GET /mercados/actual : ciclo del mercado (HU-11)
//   - GET /posiciones : tabla de posiciones en vivo (HU-05)
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

	"github.com/arquisoft/motor/internal/apuestas"
	"github.com/arquisoft/motor/internal/auth"
	"github.com/arquisoft/motor/internal/config"
	"github.com/arquisoft/motor/internal/health"
	"github.com/arquisoft/motor/internal/httpx"
	"github.com/arquisoft/motor/internal/ingest"
	"github.com/arquisoft/motor/internal/liquidacion"
	"github.com/arquisoft/motor/internal/mercados"
	"github.com/arquisoft/motor/internal/mongodb"
	"github.com/arquisoft/motor/internal/posiciones"
	"github.com/arquisoft/motor/internal/ws"
)

func main() {
	cfg, err := config.Cargar()
	if err != nil {
		log.Fatalf("configuracion invalida: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctxConexion, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := mongodb.Conectar(ctxConexion, cfg.MongoURI, cfg.MongoDB)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("conectado a MongoDB (base %s)", cfg.MongoDB)

	hub := ws.NuevoHub()
	go hub.Run(ctx)

	origenes := httpx.NuevosOrigenes(cfg.OrigenesPermitidos)

	// El JWT lo emite Pagos; el motor solo verifica la firma con el secreto
	// compartido (RNF-06). Contrato en internal/auth/contrato.go.
	validador, err := auth.NuevoValidador(cfg.JWTSecret)
	if err != nil {
		log.Fatal(err)
	}
	autenticador := auth.NuevoAutenticador(validador)

	mux := http.NewServeMux()
	health.Register(mux, db)
	ws.Register(mux, hub, autenticador, origenes.Permite)
	pagos := registrarApuestas(mux, db, cfg, autenticador)  // HU-06
	registrarMercados(ctx, mux, db, hub, cfg, autenticador) // HU-11
	// HU-07: el worker liquida los mercados cerrados cuya observacion termino.
	worker := liquidacion.NuevoWorker(liquidacion.NuevoMongoStore(db.DB), pagos, hub)
	go worker.Run(ctx)

	// HU-05: la reproduccion de HU-04 avisa al difusor, que mantiene el
	// tablero y difunde la foto de posiciones; GET /posiciones lee el tablero.
	tablero := posiciones.NuevoTablero()
	ingest.Register(mux, db, autenticador, posiciones.NuevoDifusor(tablero, hub)) // HU-04
	posiciones.Register(mux, tablero, autenticador.ExigirSesion)                  // HU-05

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           httpx.CORS(origenes, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("apagando el motor...")
		ctxApagado, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctxApagado)
	}()

	log.Printf("motor escuchando en %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("servidor HTTP termino: %v", err)
	}

	ctxCierre, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Cerrar(ctxCierre); err != nil {
		log.Printf("error al cerrar MongoDB: %v", err)
	}
}

// registrarMercados arma HU-11 (POST /mercados solo admin, GET
// /mercados/actual con cualquier sesion) y retoma los mercados que quedaron
// abiertos o en observacion antes de que el motor empiece a atender.
//
// Este paquete es el unico que cierra mercados. La liquidacion (HU-07) no usa
// el gancho svc.Liquidador: su worker (liquidacion.Worker) consulta MongoDB
// por los mercados ya cerrados cuya observacion termino, lo que ademas le
// permite reintentar si Pagos o MongoDB fallan.
func registrarMercados(ctx context.Context, mux *http.ServeMux, db *mongodb.Conexion, hub *ws.Hub, cfg config.Config, a *auth.Autenticador) {
	repo := mercados.NuevoRepoMongo(db.DB)
	svc := mercados.NuevoServicio(repo, hub, cfg.VentanaApuestas, cfg.VentanaObservacion)

	if err := svc.Iniciar(ctx); err != nil {
		log.Fatal(err)
	}
	// Despues de Iniciar: los abiertos vencidos ya se cerraron. Si falla (p. ej.
	// datos viejos con dos abiertos) el motor sigue; Abrir igual lo impide.
	ctxIndices, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := repo.AsegurarIndices(ctxIndices); err != nil {
		log.Printf("ATENCION: no se pudo crear el indice de mercado abierto unico: %v", err)
	}

	mercados.Register(mux, svc,
		func(h http.Handler) http.Handler { return a.ExigirRol(auth.RolAdmin, h) },
		a.ExigirSesion,
		func(r *http.Request) (string, bool) {
			s, ok := auth.SesionDe(r.Context())
			return s.UserID, ok
		},
	)
}

// registrarApuestas arma HU-06 (POST /apuestas, solo jugadores). Devuelve el
// cliente REST de Pagos para que el worker de liquidación (HU-07) lo reuse.
func registrarApuestas(mux *http.ServeMux, db *mongodb.Conexion, cfg config.Config, a *auth.Autenticador) *apuestas.PagosHTTP {
	pagos := apuestas.NuevoPagosHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken)
	svc := apuestas.NuevoServicio(
		apuestas.NuevosMercadosMongo(db.DB),
		pagos,
		apuestas.NuevosContextosMongo(db.DB),
	)
	apuestas.Register(mux, svc,
		func(h http.Handler) http.Handler { return a.ExigirRol(auth.RolJugador, h) },
		func(r *http.Request) (string, bool) {
			s, ok := auth.SesionDe(r.Context())
			return s.UserID, ok
		},
	)
	return pagos
}
