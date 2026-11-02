package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var db *mongo.Database

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Uso: ./ingestor [servicio|descargar]")
	}
	comando := os.Args[1]

	// 1. Conexión a MongoDB (configuración obligatoria)
	mongoURI := os.Getenv("MONGO_URI")
	mongoDB := os.Getenv("MONGO_DB")
	if mongoURI == "" || mongoDB == "" {
		log.Fatal("Faltan MONGO_URI o MONGO_DB en el entorno")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error conectando a Mongo: %v", err)
	}
	db = client.Database(mongoDB)
	fmt.Println("✅ Conectado a MongoDB")

	switch comando {
	case "descargar":
		if len(os.Args) < 3 {
			log.Fatal("Falta el ID de la sesión (session_key): ./ingestor descargar <session_key>")
		}
		DescargarCarreraCompleta(db, os.Args[2])
	case "servicio":
		IniciarServicio()
	default:
		log.Fatalf("Comando no reconocido: %s", comando)
	}
}

// HU2-19: Iniciar simulación y Healthcheck
func IniciarServicio() {
	// 1. Healthcheck
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"estado":"ok","servicio":"ingestor"}`))
		})
		puerto := os.Getenv("HTTP_PORT")
		if puerto == "" {
			puerto = "8002"
		}
		log.Printf("Healthcheck escuchando en :%s", puerto)
		http.ListenAndServe(":"+puerto, mux)
	}()

	// 2. Conexión a RabbitMQ (reintentos)
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		log.Fatal("Falta RABBITMQ_URL en el entorno")
	}
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

// Monitorea las simulaciones y arranca las que deben empezar
func monitorearSimulaciones() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var sim bson.M
		err := db.Collection("simulaciones").FindOne(ctx, bson.M{
			"estado":       "programada",
			"hora_largada": bson.M{"$lte": time.Now()},
		}).Decode(&sim)
		cancel()
		if err != nil {
			continue
		}
		go iniciarReproduccion(sim)
	}
}

// Inicia la reproducción de una simulación
func iniciarReproduccion(sim bson.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	simID := sim["_id"].(string)
	raceID := sim["carrera_id"].(string)
	tInicio := int64(0)
	if v, ok := sim["t_inicio_ms"].(int64); ok {
		tInicio = v
	}

	// Marcar como en_curso
	ahora := time.Now()
	_, err := db.Collection("simulaciones").UpdateOne(ctx,
		bson.M{"_id": simID},
		bson.M{"$set": bson.M{"estado": "en_curso", "iniciada_en": ahora}},
	)
	if err != nil {
		log.Printf("Error marcando simulación como en_curso: %v", err)
		return
	}

	// Limpiar emitido_en y simulacion_id de esa carrera
	db.Collection("eventos_carrera").UpdateMany(ctx,
		bson.M{"carrera_id": raceID},
		bson.M{"$unset": bson.M{"emitido_en": "", "simulacion_id": ""}},
	)

	// Marcar en reproducciones (para workers viejos)
	db.Collection("reproducciones").UpdateOne(ctx,
		bson.M{"_id": raceID},
		bson.M{"$set": bson.M{
			"iniciada_en":  ahora,
			"latido_en":    ahora,
			"terminada_en": nil,
			"factor":       1.0,
		}},
		options.Update().SetUpsert(true),
	)

	// Bucle de reproducción
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	tAnterior := tInicio

	for range ticker.C {
		ahora := time.Now()
		// Reloj de carrera: t = t_inicio_ms + (ahora - iniciada_en)
		iniciadaEn := ahora // Usar la hora local que ya guardamos
		t := tInicio + time.Since(iniciadaEn).Milliseconds()

		if t <= tAnterior {
			continue
		}

		// Actualizar eventos que ya deben salir
		_, err := db.Collection("eventos_carrera").UpdateMany(ctx,
			bson.M{
				"carrera_id":   raceID,
				"t_carrera_ms": bson.M{"$gt": tAnterior, "$lte": t},
			},
			bson.M{"$set": bson.M{"emitido_en": ahora, "simulacion_id": simID}},
		)
		if err != nil {
			log.Printf("Error actualizando eventos: %v", err)
		}

		// Actualizar progreso en la simulación (vuelta_actual y total_vueltas)
		// NOTA: Ajustar el campo de vuelta según el formato de OpenF1.
		// Por ahora, dejamos vuelta_actual en 0.
		vueltaActual := 0
		db.Collection("simulaciones").UpdateOne(ctx,
			bson.M{"_id": simID},
			bson.M{"$set": bson.M{
				"t_carrera_ms":  t,
				"latido_en":     ahora,
				"vuelta_actual": vueltaActual,
			}},
		)
		db.Collection("reproducciones").UpdateOne(ctx,
			bson.M{"_id": raceID},
			bson.M{"$set": bson.M{"latido_en": ahora}},
		)

		tAnterior = t

		// Verificar si ya pasó el último evento
		count, _ := db.Collection("eventos_carrera").CountDocuments(ctx, bson.M{
			"carrera_id":   raceID,
			"t_carrera_ms": bson.M{"$gt": t},
		})
		if count == 0 {
			// Finalizar
			db.Collection("simulaciones").UpdateOne(ctx,
				bson.M{"_id": simID},
				bson.M{"$set": bson.M{"estado": "finalizada", "terminada_en": ahora}},
			)
			db.Collection("reproducciones").UpdateOne(ctx,
				bson.M{"_id": raceID},
				bson.M{"$set": bson.M{"terminada_en": ahora}},
			)
			fmt.Printf("✅ Simulación %s finalizada\n", simID)
			return
		}
	}
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
