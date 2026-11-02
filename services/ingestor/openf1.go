package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

type OpenF1Record map[string]interface{}

func DescargarCarreraCompleta(db *mongo.Database, sessionKey string) {
	fmt.Printf("🚀 Iniciando descarga de la sesión %s\n", sessionKey)
	collection := db.Collection("eventos_carrera")

	// 1. Obtener la hora de inicio de la sesión
	sessions := fetchOpenF1Data("sessions?session_key=" + sessionKey)
	if len(sessions) == 0 {
		log.Fatal("No se encontró la sesión en OpenF1")
	}
	dateStartStr, ok := sessions[0]["date_start"].(string)
	if !ok {
		log.Fatal("La sesión no tiene fecha de inicio (date_start)")
	}
	horaInicio, err := time.Parse(time.RFC3339, dateStartStr)
	if err != nil {
		log.Fatalf("Error parseando fecha de inicio: %v", err)
	}
	fmt.Printf("⏱️ Largada de la carrera: %s\n", dateStartStr)

	// Función auxiliar para calcular el timestamp real
	calcTimestamp := func(dateStr string) time.Time {
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return horaInicio
		}
		return t
	}

	// 2. Definir los endpoints con sus tipos REALES (Actualizamos el validador)
	endpoints := []struct {
		Name string
		Tipo string
		URL  string
	}{
		{"Posiciones", "posicion", "position?session_key=" + sessionKey},
		{"Vueltas", "vuelta", "laps?session_key=" + sessionKey},
		{"Pits", "pit_stop", "pit?session_key=" + sessionKey},
		{"Clima", "clima", "weather?session_key=" + sessionKey},
		{"Ubicaciones", "ubicacion", "location?session_key=" + sessionKey}, // Para William
		{"Intervalos", "intervalo", "intervals?session_key=" + sessionKey}, // Para Daniel
		{"Control de Carrera", "bandera", "race_control?session_key=" + sessionKey},
	}

	// 3. Descargar y guardar cada uno
	for _, ep := range endpoints {
		fmt.Printf("⬇️ Descargando %s...\n", ep.Name)

		data := fetchOpenF1Data(ep.URL)

		if len(data) == 0 {
			fmt.Printf("⚠️ No se encontraron datos para %s\n", ep.Name)
			continue
		}

		var docs []interface{}
		for _, item := range data {
			var dateStr string
			if d, ok := item["date"].(string); ok {
				dateStr = d
			} else if d, ok := item["date_start"].(string); ok {
				dateStr = d
			}

			timestamp := calcTimestamp(dateStr)

			doc := map[string]interface{}{
				"carrera_id": sessionKey,
				"timestamp":  timestamp,
				"tipo":       ep.Tipo,
				"datos": map[string]interface{}{
					"t_carrera_ms": timestamp.Sub(horaInicio).Milliseconds(),
					"raw":          item,
				},
			}
			docs = append(docs, doc)
		}

		// Insertamos en bloques de 1000
		if len(docs) > 0 {
			batchSize := 1000
			for i := 0; i < len(docs); i += batchSize {
				end := i + batchSize
				if end > len(docs) {
					end = len(docs)
				}
				batch := docs[i:end]

				_, err := collection.InsertMany(context.Background(), batch)
				if err != nil {
					log.Printf("❌ Error insertando bloque de %s en Mongo: %v", ep.Name, err)
				}
			}
			fmt.Printf("✅ %s: %d documentos guardados.\n", ep.Name, len(docs))
		}
	}
	fmt.Println("🎉 ¡Carrera descargada en 'apuestas_db.eventos_carrera'!")
}

func fetchOpenF1Data(endpoint string) []OpenF1Record {
	url := "https://api.openf1.org/v1/" + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creando petición HTTP: %v", err)
		return nil
	}

	apiKey := os.Getenv("OPENF1_API_KEY")
	if apiKey == "" {
		apiKey = "TU_TOKEN_AQUI"
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error en petición HTTP a %s: %v", url, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("API OpenF1 devolvió status %d para %s", resp.StatusCode, url)
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	var records []OpenF1Record
	json.Unmarshal(body, &records)
	return records
}
