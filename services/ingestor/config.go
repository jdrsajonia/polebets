package main

import (
	"fmt"
	"os"
	"strings"
)

// Config reúne la configuración del Ingestor, leída del entorno.
type Config struct {
	MongoURI     string
	MongoDB      string
	RabbitURL    string
	HTTPPort     string
	OpenF1URL    string
	OpenF1APIKey string
}

// cargarConfig lee el entorno. MONGO_URI y MONGO_DB son obligatorias siempre;
// RABBITMQ_URL solo para el modo servicio (descargar no usa el Broker). Sin
// valores por defecto para las conexiones: si falta una, no arranca (mismo
// criterio del Motor y del Orquestador).
func cargarConfig(conBroker bool) (Config, error) {
	cfg := Config{
		MongoURI:     strings.TrimSpace(os.Getenv("MONGO_URI")),
		MongoDB:      strings.TrimSpace(os.Getenv("MONGO_DB")),
		RabbitURL:    strings.TrimSpace(os.Getenv("RABBITMQ_URL")),
		HTTPPort:     strings.TrimSpace(os.Getenv("HTTP_PORT")),
		OpenF1URL:    strings.TrimSpace(os.Getenv("OPENF1_BASE_URL")),
		OpenF1APIKey: strings.TrimSpace(os.Getenv("OPENF1_API_KEY")),
	}
	var faltan []string
	if cfg.MongoURI == "" {
		faltan = append(faltan, "MONGO_URI")
	}
	if cfg.MongoDB == "" {
		faltan = append(faltan, "MONGO_DB")
	}
	if conBroker && cfg.RabbitURL == "" {
		faltan = append(faltan, "RABBITMQ_URL")
	}
	if len(faltan) > 0 {
		return Config{}, fmt.Errorf("faltan variables de entorno: %s", strings.Join(faltan, ", "))
	}
	if cfg.HTTPPort == "" {
		cfg.HTTPPort = "8002"
	}
	if cfg.OpenF1URL == "" {
		cfg.OpenF1URL = "https://api.openf1.org/v1"
	}
	cfg.OpenF1URL = strings.TrimRight(cfg.OpenF1URL, "/")
	return cfg, nil
}
