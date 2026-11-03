package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// db es la base de Mongo del Ingestor (único servicio que escribe en ella, RNF-18).
var db *mongo.Database

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Uso: ingestor servicio | ingestor descargar <session_key>")
	}
	comando := os.Args[1]

	cfg, err := cargarConfig(comando == "servicio")
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	cancel()
	if err != nil {
		log.Fatalf("Error conectando a Mongo: %v", err)
	}
	db = client.Database(cfg.MongoDB)
	fmt.Println("✅ Conectado a MongoDB")

	switch comando {
	case "descargar":
		if len(os.Args) < 3 {
			log.Fatal("Falta el ID de la sesión (session_key): ingestor descargar <session_key>")
		}
		// Ctrl+C (o docker stop) cancela la descarga y se limpia lo escrito.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := DescargarCarrera(ctx, db, nuevoClienteOpenF1(cfg), os.Args[2]); err != nil {
			stop()
			log.Fatalf("❌ La descarga falló: %v", err)
		}
	case "servicio":
		IniciarServicio(cfg)
	default:
		log.Fatalf("Comando no reconocido: %s", comando)
	}
}
