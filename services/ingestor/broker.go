package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
)

// HU2-19: Iniciar simulación y Healthcheck
func IniciarServicio(cfg Config) {
	// 1. Healthcheck
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"estado":"ok","servicio":"ingestor"}`))
		})
		puerto := cfg.HTTPPort
		log.Printf("Healthcheck escuchando en :%s", puerto)
		http.ListenAndServe(":"+puerto, mux)
	}()

	// 2. Conexión a RabbitMQ (reintentos)
	rabbitURL := cfg.RabbitURL
	var conn *amqp.Connection
	var err error
	for i := 0; i < 10; i++ {
		conn, err = amqp.Dial(rabbitURL)
		if err == nil {
			break
		}
		log.Printf("Reintentando conexión a RabbitMQ (%d/10): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Error conectando a RabbitMQ tras reintentos: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Error abriendo canal RabbitMQ: %v", err)
	}

	// Cola de órdenes y de consultas
	ch.QueueDeclare("ingestor.commands", true, false, false, false, nil)
	ch.QueueDeclare("ingestor.consultas", true, false, false, false, nil)

	// Consumidor de órdenes (ack manual)
	msgs, err := ch.Consume("ingestor.commands", "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Error consumiendo cola: %v", err)
	}

	// Goroutine de monitoreo de simulaciones
	go monitorearSimulaciones()

	fmt.Println("🎧 Ingestor escuchando órdenes del Admin en RabbitMQ...")

	for msg := range msgs {
		var comando map[string]interface{}
		if err := json.Unmarshal(msg.Body, &comando); err != nil {
			log.Printf("Error parseando mensaje: %v", err)
			msg.Nack(false, false)
			continue
		}
		fmt.Printf("📥 Orden recibida: %+v\n", comando)

		action, _ := comando["action"].(string)
		switch action {
		case "start_simulation":
			procesarStartSimulation(ch, msg, comando)
		case "list_races":
			procesarListRaces(ch, msg)
		default:
			log.Printf("Acción no reconocida: %s", action)
			msg.Nack(false, false)
		}
	}
}

// Procesa la orden de iniciar simulación
func procesarStartSimulation(ch *amqp.Channel, msg amqp.Delivery, comando map[string]interface{}) {
	raceID, _ := comando["race_id"].(string)
	minutos, _ := comando["minutos_anticipacion"].(float64)
	if minutos <= 0 {
		minutos = 10
	}

	// Verificar que la carrera exista en 'carreras'
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := db.Collection("carreras").FindOne(ctx, bson.M{"carrera_id": raceID}).Err()
	if err != nil {
		responderError(ch, msg, "carrera_no_descargada")
		return
	}

	// Verificar que no haya una simulación programada o en curso
	count, err := db.Collection("simulaciones").CountDocuments(ctx, bson.M{
		"estado": bson.M{"$in": []string{"programada", "en_curso"}},
	})
	if err != nil {
		responderError(ch, msg, "error_interno")
		return
	}
	if count > 0 {
		responderError(ch, msg, "ya_hay_simulacion")
		return
	}

	// Crear simulación
	simID := uuid.New().String()
	ahora := time.Now()
	doc := bson.M{
		"_id":          simID,
		"carrera_id":   raceID,
		"estado":       "programada",
		"hora_largada": ahora.Add(time.Duration(minutos) * time.Minute),
		"t_inicio_ms":  int64(0),
		"creada_en":    ahora,
	}
	_, err = db.Collection("simulaciones").InsertOne(ctx, doc)
	if err != nil {
		log.Printf("Error insertando simulación: %v", err)
		responderError(ch, msg, "error_interno")
		return
	}

	// Responder al Admin
	resp := bson.M{"ok": true, "simulacion_id": simID, "hora_largada": doc["hora_largada"]}
	responder(ch, msg, resp)
	fmt.Printf("✅ Simulación programada: %s para %s\n", simID, raceID)
}

// Procesa la lista de carreras
func procesarListRaces(ch *amqp.Channel, msg amqp.Delivery) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cursor, err := db.Collection("carreras").Find(ctx, bson.M{})
	if err != nil {
		responderError(ch, msg, "error_interno")
		return
	}
	defer cursor.Close(ctx)
	var carreras []bson.M
	cursor.All(ctx, &carreras)
	responder(ch, msg, bson.M{"ok": true, "carreras": carreras})
}

// Utilidades para responder
func responder(ch *amqp.Channel, msg amqp.Delivery, body interface{}) {
	bodyBytes, _ := json.Marshal(body)
	if msg.ReplyTo != "" {
		ch.Publish("", msg.ReplyTo, false, false, amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: msg.CorrelationId,
			Body:          bodyBytes,
		})
	}
	msg.Ack(false)
}

func responderError(ch *amqp.Channel, msg amqp.Delivery, errCode string) {
	responder(ch, msg, bson.M{"ok": false, "error": errCode})
}
