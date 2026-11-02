package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Colas del Broker (contrato «Colas y mensajes del Broker»). Órdenes y
// consultas van separadas: una consulta no espera detrás de una orden.
const (
	colaOrdenes   = "ingestor.commands"  // start_simulation (mensajería de comando)
	colaConsultas = "ingestor.consultas" // list_races (petición-respuesta)

	accionIniciar = "start_simulation"
	accionListar  = "list_races"

	esperaMaxBroker = 30 * time.Second
)

// mensajeBroker es el cuerpo de las órdenes y consultas que publica Admin.
// correlation_id y reply_to viajan en las propiedades AMQP.
type mensajeBroker struct {
	Action    string `json:"action"`
	RaceID    string `json:"race_id"`
	Minutos   int    `json:"minutos_anticipacion"`
	TInicioMs int64  `json:"t_inicio_ms"`
}

// IniciarServicio arranca el healthcheck, el monitor de simulaciones y el
// consumidor del Broker, hasta recibir SIGTERM/SIGINT.
func IniciarServicio(cfg Config) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rep := &reproductor{db: db}
	if err := rep.marcarHuerfanas(ctx); err != nil {
		log.Fatalf("❌ %v", err)
	}

	srv := servidorSalud(cfg.HTTPPort)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("❌ healthcheck: %v", err)
		}
	}()

	terminado := make(chan struct{})
	go func() {
		rep.monitorear(ctx)
		close(terminado)
	}()

	atenderBroker(ctx, cfg.RabbitURL, almacenMongo{db: db})

	<-terminado
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(c); err != nil {
		log.Printf("cerrando el healthcheck: %v", err)
	}
	log.Println("👋 Ingestor detenido")
}

func servidorSalud(puerto string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"estado":"ok","servicio":"ingestor"}`))
	})
	log.Printf("Healthcheck escuchando en :%s", puerto)
	return &http.Server{Addr: ":" + puerto, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// atenderBroker mantiene la conexión con RabbitMQ: si no está listo o se cae,
// reintenta con espera creciente. Sin Broker el resto del Ingestor sigue (la
// reproducción en curso no depende de él, RNF-16).
func atenderBroker(ctx context.Context, url string, alm almacenSimulaciones) {
	espera := time.Second
	for ctx.Err() == nil {
		inicio := time.Now()
		err := consumir(ctx, url, alm)
		if ctx.Err() != nil {
			return
		}
		if time.Since(inicio) > esperaMaxBroker {
			espera = time.Second
		}
		log.Printf("⚠️ Broker: %v; reintento en %v", err, espera)
		if dormir(ctx, espera) != nil {
			return
		}
		espera = min(espera*2, esperaMaxBroker)
	}
}

// consumir atiende las dos colas en una sola goroutine: los mensajes se
// procesan de a uno y se confirman (ack) después de procesarlos y responder.
func consumir(ctx context.Context, url string, alm almacenSimulaciones) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("conectando: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abriendo canal: %w", err)
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("configurando prefetch: %w", err)
	}
	for _, cola := range []string{colaOrdenes, colaConsultas} {
		if _, err := ch.QueueDeclare(cola, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declarando %s: %w", cola, err)
		}
	}
	ordenes, err := ch.Consume(colaOrdenes, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumiendo %s: %w", colaOrdenes, err)
	}
	consultas, err := ch.Consume(colaConsultas, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumiendo %s: %w", colaConsultas, err)
	}
	cerrado := conn.NotifyClose(make(chan *amqp.Error, 1))
	log.Printf("🎧 Escuchando %s y %s", colaOrdenes, colaConsultas)

	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-cerrado:
			return fmt.Errorf("conexión cerrada: %v", e)
		case msg, ok := <-ordenes:
			if !ok {
				return errors.New("se cerró el consumidor de órdenes")
			}
			atender(ctx, ch, msg, accionIniciar, alm)
		case msg, ok := <-consultas:
			if !ok {
				return errors.New("se cerró el consumidor de consultas")
			}
			atender(ctx, ch, msg, accionListar, alm)
		}
	}
}

// atender procesa un mensaje de la cola que espera `accion`. Los mensajes
// malformados se responden con error y se confirman igual: reencolarlos solo
// los haría volver a fallar.
func atender(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, accion string, alm almacenSimulaciones) {
	var m mensajeBroker
	var resp any
	switch err := json.Unmarshal(msg.Body, &m); {
	case err != nil:
		log.Printf("⚠️ mensaje inválido en %s: %v", msg.RoutingKey, err)
		resp = respuestaSimulacion{Error: errOrdenInvalida}
	case m.Action != accion:
		log.Printf("⚠️ acción %q no va en %s", m.Action, msg.RoutingKey)
		resp = respuestaSimulacion{Error: errOrdenInvalida}
	case accion == accionIniciar:
		log.Printf("📥 Orden %s: carrera %s, %d min", m.Action, m.RaceID, m.Minutos)
		resp = programarSimulacion(ctx, alm, ordenSimulacion{CarreraID: m.RaceID, Minutos: m.Minutos, TInicioMs: m.TInicioMs}, time.Now())
	default:
		carreras, err := listarCarreras(ctx, db)
		if err != nil {
			log.Printf("❌ %v", err)
			resp = respuestaSimulacion{Error: errInterno}
		} else {
			resp = struct {
				OK       bool             `json:"ok"`
				Carreras []carreraResumen `json:"carreras"`
			}{true, carreras}
		}
	}

	if err := responder(ctx, ch, msg, resp); err != nil {
		log.Printf("❌ respondiendo a %s: %v", msg.ReplyTo, err)
	}
	if err := msg.Ack(false); err != nil {
		log.Printf("❌ ack: %v", err)
	}
}

// responder publica en reply_to con el mismo correlation_id (petición-respuesta).
func responder(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, cuerpo any) error {
	if msg.ReplyTo == "" {
		return nil
	}
	b, err := json.Marshal(cuerpo)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeoutOp)
	defer cancel()
	return ch.PublishWithContext(c, "", msg.ReplyTo, false, false, amqp.Publishing{
		ContentType:   "application/json",
		CorrelationId: msg.CorrelationId,
		Body:          b,
	})
}
