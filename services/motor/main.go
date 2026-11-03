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
//   - worker HU2-08: abre, cierra y resuelve las dinamicas en Pagos
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arquisoft/motor/internal/apostar"
	"github.com/arquisoft/motor/internal/apuestas"
	"github.com/arquisoft/motor/internal/auth"
	"github.com/arquisoft/motor/internal/carrera"
	"github.com/arquisoft/motor/internal/cierre"
	"github.com/arquisoft/motor/internal/config"
	"github.com/arquisoft/motor/internal/dinamicas"
	"github.com/arquisoft/motor/internal/fichas"
	"github.com/arquisoft/motor/internal/health"
	"github.com/arquisoft/motor/internal/httpx"
	"github.com/arquisoft/motor/internal/liquidacion"
	"github.com/arquisoft/motor/internal/liquidar"
	"github.com/arquisoft/motor/internal/mercados"
	"github.com/arquisoft/motor/internal/mongodb"
	"github.com/arquisoft/motor/internal/posiciones"
	"github.com/arquisoft/motor/internal/ws"

	"github.com/redis/go-redis/v9"
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

	// HU2-11: almacen de fichas y ranking del torneo en Redis. Es opcional:
	// sin REDIS_URL el motor arranca igual y las apuestas de torneo no se
	// liquidan (quedan reintentables). Asi una demo sin torneo no necesita
	// Redis y el resto de HU no se ve afectado.
	liquidadorTorneo, cerrarRedis := construirTorneo(ctx, cfg, hub)
	if cerrarRedis != nil {
		defer cerrarRedis()
	}

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
	pagos := registrarApuestas(mux, db, cfg, autenticador) // HU-06 (P1; la reemplaza HU2-09)
	// HU2-14 lee la carrera del Ingestor en Mongo; HU2-09 usa la misma fuente
	// para guardar la vuelta de cada apuesta (bets.lap).
	fuenteCarrera := carrera.NuevaMongoFuente(db.DB)
	// HU2-09: POST /internal/apuestas, la llama el Orquestador. Con Redis, las
	// dinamicas de un inscrito van en fichas del torneo (almacen de HU2-11).
	svcApostar := apostar.NuevoServicio(apostar.NuevoPagosHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken), cfg.MaxApuestaIndividual)
	if t, ok := liquidadorTorneo.(adaptadorTorneo); ok {
		svcApostar.Fichas = fichasApuesta{almacen: t.almacen}
	}
	svcApostar.Carrera = vueltaCarrera{fuente: fuenteCarrera}
	apostar.Register(mux, svcApostar, cfg.InternalAPIToken)
	// HU2-10: liquida los mercados del P2 (Pagos) que ya tienen opcion
	// ganadora: las individuales en tokens y, con Redis, las de torneo en
	// fichas (almacen de HU2-11). Sin Redis las de torneo esperan.
	workerP2 := liquidar.NuevoWorker(
		liquidar.NuevoPagosHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken),
		avisoWS{hub: hub},
		5*time.Second,
	)
	if t, ok := liquidadorTorneo.(adaptadorTorneo); ok {
		workerP2.Fichas = t
		// HU2-12: con la carrera finalizada y nada pendiente en Pagos, reparte
		// el botin y limpia Redis. Sin Redis no hay torneo que cerrar.
		go cierre.NuevoWorker(
			cierreTorneo{adaptadorTorneo: t, pagos: fichas.NuevoPagosCierreHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken)},
			cierre.NuevoPagosHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken),
			simulacionCarrera{fuente: fuenteCarrera},
			5*time.Second,
		).Run(ctx)
	}
	go workerP2.Run(ctx)
	// HU2-08: el Gestor de mercados dinamicos lee la carrera que reproduce el
	// Ingestor (solo lectura, RNF-18) y guarda los mercados en Pagos. Todo se
	// decide con el tiempo de carrera (RNF-19); event_id = _id de la simulacion.
	go dinamicas.NuevoWorker(
		dinamicas.NuevoLectorMongo(db.DB),
		dinamicas.NuevoPagosHTTP(cfg.PagosBaseURL, cfg.InternalAPIToken),
		difusionWS{hub: hub},
		cfg.VentanaApuestas,
		cfg.VentanaObservacion,
	).Run(ctx)
	registrarMercados(ctx, mux, db, hub, cfg, autenticador) // HU-11
	// HU-07: el worker liquida los mercados cerrados cuya observacion termino.
	// HU2-11: si hay almacen de fichas, el worker tambien liquida las apuestas
	// de torneo en fichas (Redis) y difunde el ranking.
	worker := liquidacion.NuevoWorker(liquidacion.NuevoMongoStore(db.DB), pagos, hub)
	if liquidadorTorneo != nil {
		worker.ConLiquidadorTorneo(liquidadorTorneo)
	}
	go worker.Run(ctx)

	// HU-05: la reproduccion de HU-04 avisa al difusor, que mantiene el
	// tablero y difunde la foto de posiciones; GET /posiciones lee el tablero.
	tablero := posiciones.NuevoTablero()
	posiciones.Register(mux, tablero, autenticador.ExigirSesion) // HU-05
	// HU2-14: lee de Mongo la carrera que reproduce el Ingestor y alimenta el
	// tablero; cada cambio sale por WebSocket ("posiciones").
	go carrera.NuevoLector(
		fuenteCarrera,
		tableroEnVivo{posiciones.NuevoDifusor(tablero, hub)},
		500*time.Millisecond,
	).Run(ctx)

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

// construirTorneo arma el almacen de fichas del torneo sobre Redis (HU2-11) y
// el adaptador que el worker de liquidacion usa para pagar en fichas. Si no
// hay REDIS_URL devuelve (nil, nil): el motor funciona sin torneo. El segundo
// valor es una funcion para cerrar la conexion al apagar.
func construirTorneo(ctx context.Context, cfg config.Config, hub *ws.Hub) (liquidacion.LiquidadorTorneo, func()) {
	if cfg.RedisURL == "" {
		log.Println("HU2-11: sin REDIS_URL, el torneo en fichas queda deshabilitado")
		return nil, nil
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Printf("HU2-11: REDIS_URL invalida, torneo deshabilitado: %v", err)
		return nil, nil
	}
	rdb := redis.NewClient(opts)
	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctxPing).Err(); err != nil {
		log.Printf("HU2-11: Redis no responde, torneo deshabilitado: %v", err)
		_ = rdb.Close()
		return nil, nil
	}
	log.Println("HU2-11: conectado a Redis para fichas y ranking del torneo")

	// Las claves del torneo expiran a las 24 h por si el cierre (HU2-12) no
	// llega a limpiarlas; una carrera dura minutos, asi que no afecta la demo.
	almacen := fichas.NuevoAlmacen(fichas.NuevoRedisStore(rdb, 24*time.Hour))
	difusor := fichas.NuevoDifusor(almacen, hub, fichas.ParametrosTorneo{
		FichasIniciales: cfg.FichasIniciales,
		MinWagerPct:     cfg.MinApostadoPct,
	})
	return adaptadorTorneo{almacen: almacen, difusor: difusor}, func() { _ = rdb.Close() }
}

// adaptadorTorneo cumple liquidacion.LiquidadorTorneo traduciendo entre el
// worker y el almacen de fichas (HU2-11). Vive en main porque conecta dos
// paquetes que no deben conocerse entre si (fichas no importa liquidacion).
type adaptadorTorneo struct {
	almacen *fichas.Almacen
	difusor *fichas.Difusor
}

func (a adaptadorTorneo) Liquidar(ctx context.Context, eventoID, apuestaID string, gano bool, multiplicador float64) (liquidacion.ResultadoTorneo, error) {
	res, err := a.almacen.LiquidarYUbicar(ctx, eventoID, apuestaID, gano, multiplicador)
	if err != nil {
		return liquidacion.ResultadoTorneo{}, err
	}
	return liquidacion.ResultadoTorneo{
		FichasGanadas: res.FichasGanadas,
		FichasTotal:   res.FichasTotal,
		Puesto:        res.Puesto,
	}, nil
}

// LiquidarApuesta cumple liquidar.Fichas (HU2-10 con los mercados del P2).
// Solo la reserva inexistente (Redis respondio) es ErrSinReserva; el resto
// son fallos de conexion que el worker reintenta.
func (a adaptadorTorneo) LiquidarApuesta(ctx context.Context, eventoID, apuestaID string, gano bool, multiplicador float64) (liquidar.FichasTorneo, error) {
	res, err := a.almacen.LiquidarYUbicar(ctx, eventoID, apuestaID, gano, multiplicador)
	if errors.Is(err, fichas.ErrJugadorNoInscrito) {
		return liquidar.FichasTorneo{}, liquidar.ErrSinReserva
	}
	if err != nil {
		return liquidar.FichasTorneo{}, err
	}
	return liquidar.FichasTorneo{Ganadas: res.FichasGanadas, Total: res.FichasTotal, Puesto: res.Puesto}, nil
}

func (a adaptadorTorneo) Difundir(ctx context.Context, eventoID string) {
	a.difusor.Difundir(ctx, eventoID)
}

// cierreTorneo cumple cierre.Torneos sobre el almacen de fichas (HU2-12).
type cierreTorneo struct {
	adaptadorTorneo
	pagos fichas.PagosCierre
}

func (c cierreTorneo) EventosActivos(ctx context.Context) ([]string, error) {
	return c.almacen.EventosActivos(ctx)
}

func (c cierreTorneo) DifundirRanking(ctx context.Context, eventoID string) {
	c.difusor.Difundir(ctx, eventoID)
}

func (c cierreTorneo) Cerrar(ctx context.Context, eventoID string) error {
	_, err := c.almacen.CerrarTorneo(ctx, eventoID, c.pagos)
	return err
}

// simulacionCarrera cumple cierre.Carrera con la fuente de HU2-14.
type simulacionCarrera struct {
	fuente *carrera.MongoFuente
}

func (s simulacionCarrera) EstadoSimulacion(ctx context.Context, carreraID string) (string, bool, error) {
	sim, ok, err := s.fuente.SimulacionDe(ctx, carreraID)
	return sim.Estado, ok, err
}

// tableroEnVivo adapta el Difusor de HU-05 a carrera.Tablero (HU2-14). El
// paquete carrera no importa posiciones ni ws para poder probarse sin ellos.
type tableroEnVivo struct {
	difusor *posiciones.Difusor
}

func (t tableroEnVivo) Iniciar(carreraID string, pilotos []carrera.Piloto) {
	datos := make([]posiciones.PilotoCarrera, 0, len(pilotos))
	for _, p := range pilotos {
		datos = append(datos, posiciones.PilotoCarrera{Numero: p.Numero, Nombre: p.Nombre, Codigo: p.Codigo, Equipo: p.Equipo, Color: p.Color})
	}
	t.difusor.Iniciar(carreraID, datos, map[int]int{})
}

func (t tableroEnVivo) Aplicar(cambios map[int]int, vuelta carrera.Vuelta) {
	t.difusor.Aplicar(cambios, vuelta.Actual, vuelta.Total)
}

func (t tableroEnVivo) Reenviar() {
	t.difusor.Reenviar()
}

// avisoWS adapta el Hub del WebSocket a liquidar.Avisador (HU2-10). El
// paquete liquidar no importa ws para poder probarse sin dependencias.
type avisoWS struct {
	hub *ws.Hub
}

func (a avisoWS) EnviarA(userID, tipo string, payload any) error {
	return a.hub.EnviarA(userID, ws.Mensaje{Type: tipo, Payload: payload})
}

// vueltaCarrera cumple apostar.Carrera con la fuente de HU2-14: la vuelta de
// la simulacion en curso (simulaciones.vuelta_actual). Antes de la largada o
// con la carrera terminada no hay vuelta y bets.lap queda nula.
//
// No compara la carrera de la simulacion con la del evento del mercado: RF-22
// solo permite una simulacion a la vez, asi que la que esta en curso es la
// del evento.
type vueltaCarrera struct {
	fuente *carrera.MongoFuente
}

func (v vueltaCarrera) VueltaActual(ctx context.Context) (int, bool, error) {
	sim, ok, err := v.fuente.SimulacionActual(ctx)
	if err != nil || !ok || sim.Estado != "en_curso" || sim.Vuelta.Actual <= 0 {
		return 0, false, err
	}
	return sim.Vuelta.Actual, true, nil
}

// difusionWS adapta el Hub a dinamicas.Difusor (HU2-08), por la misma razon
// que avisoWS.
type difusionWS struct {
	hub *ws.Hub
}

func (d difusionWS) Difundir(tipo string, payload any) error {
	return d.hub.Difundir(ws.Mensaje{Type: tipo, Payload: payload})
}

// fichasApuesta cumple apostar.Fichas sobre el almacen de fichas (HU2-09 con
// HU2-11). Vive en main por la misma razon que adaptadorTorneo.
type fichasApuesta struct {
	almacen *fichas.Almacen
}

// Jugador busca al usuario entre los inscritos del torneo. Usa Estados y no
// EstadoDe porque EstadoDe recalcula y agregaria al ranking a quien no esta
// inscrito.
func (f fichasApuesta) Jugador(ctx context.Context, eventoID, userID string) (apostar.EstadoFichas, bool, error) {
	estados, err := f.almacen.Estados(ctx, eventoID)
	if err != nil {
		return apostar.EstadoFichas{}, false, fmt.Errorf("%w: fichas: %v", apostar.ErrPagosNoDisponible, err)
	}
	for _, e := range estados {
		if e.UsuarioID == userID {
			return apostar.EstadoFichas{Fichas: e.Fichas, Apostado: e.Apostado}, true, nil
		}
	}
	return apostar.EstadoFichas{}, false, nil
}

func (f fichasApuesta) Reservar(ctx context.Context, eventoID string, r apostar.ReservaFichas) (apostar.EstadoFichas, error) {
	e, err := f.almacen.ReservarApuesta(ctx, eventoID, fichas.Reserva{
		ApuestaID: r.ApuestaID,
		UsuarioID: r.UserID,
		Monto:     r.Monto,
		Opcion:    r.Opcion,
		Multiplic: r.Multiplicador,
	})
	switch {
	case err == nil:
		return apostar.EstadoFichas{Fichas: e.Fichas, Apostado: e.Apostado}, nil
	case errors.Is(err, fichas.ErrFichasInsuficientes):
		return apostar.EstadoFichas{}, apostar.ErrFichasInsuficientes
	case errors.Is(err, fichas.ErrReservaIncompatible):
		return apostar.EstadoFichas{}, apostar.ErrYaApostaste
	case errors.Is(err, fichas.ErrPeticionInvalida):
		return apostar.EstadoFichas{}, apostar.ErrPeticionInvalida
	default: // torneo no activo (se cerro entre la consulta y la reserva) o Redis caido
		return apostar.EstadoFichas{}, fmt.Errorf("%w: fichas: %v", apostar.ErrPagosNoDisponible, err)
	}
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
