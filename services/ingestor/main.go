package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Uso: ./ingestor [servicio|descargar]")
	}
	comando := os.Args[1]

	// 1. Conexión a MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error conectando a Mongo: %v", err)
	}
	db := client.Database("apuestas_db")
	fmt.Println("✅ Conectado a MongoDB")

	switch comando {
	case "descargar":
		if len(os.Args) < 3 {
			log.Fatal("Falta el ID de la sesión (session_key): ./ingestor descargar <session_key>")
		}
		DescargarCarreraCompleta(db, os.Args[2])
	case "servicio":
		IniciarServicio(db, ctx)
	default:
		log.Fatalf("Comando no reconocido: %s", comando)
	}
}

// HU2-19: Iniciar simulación y Healthcheck
func IniciarServicio(db *mongo.Database, ctx context.Context) {
	// 1. Levantar Healthcheck (Obligatorio para Docker)
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

	// 2. Conexión a RabbitMQ para escuchar ordenes del Admin
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("Error conectando a RabbitMQ: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Error abriendo canal RabbitMQ: %v", err)
	}

	// Declaramos la cola donde el Admin nos enviará las órdenes
	colaOrdenes := "ingestor.commands"
	_, err = ch.QueueDeclare(colaOrdenes, true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Error declarando cola: %v", err)
	}

	msgs, err := ch.Consume(colaOrdenes, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Error consumiendo cola: %v", err)
	}

	fmt.Println("🎧 Ingestor escuchando órdenes del Admin en RabbitMQ...")

	for msg := range msgs {
		var comando map[string]interface{}
		if err := json.Unmarshal(msg.Body, &comando); err == nil {
			fmt.Printf("📥 Orden recibida: %+v\n", comando)
			if comando["action"] == "start_simulation" {
				db.Collection("simulaciones").InsertOne(ctx, bson.M{
					"race_id":      comando["race_id"],
					"status":       "scheduled",
					"hora_largada": time.Now().Add(10 * time.Minute),
				})
				fmt.Println("✅ Simulación programada en Mongo")
			}
		}
	}
}
