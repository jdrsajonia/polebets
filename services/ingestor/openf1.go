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

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OpenF1Record map[string]interface{}

func DescargarCarreraCompleta(db *mongo.Database, sessionKey string) {
	fmt.Printf("🚀 Iniciando descarga de la sesión %s\n", sessionKey)
	ctx := context.Background()

	// 1. Borrar eventos viejos y carrera anterior (idempotencia)
	_, _ = db.Collection("eventos_carrera").DeleteMany(ctx, bson.M{"carrera_id": sessionKey})
	_, _ = db.Collection("carreras").DeleteOne(ctx, bson.M{"carrera_id": sessionKey})

	// 2. Obtener sesión
	sessions := fetchOpenF1Data("sessions?session_key=" + sessionKey)
	if len(sessions) == 0 {
		log.Fatal("No se encontró la sesión en OpenF1")
	}
	session := sessions[0]
	dateStartStr, ok := session["date_start"].(string)
	if !ok {
		log.Fatal("La sesión no tiene fecha de inicio (date_start)")
	}
	horaInicio, err := time.Parse(time.RFC3339, dateStartStr)
	if err != nil {
		log.Fatalf("Error parseando fecha de inicio: %v", err)
	}
	fmt.Printf("⏱️ Largada de la carrera: %s\n", dateStartStr)

	// 3. Descargar pilotos (/drivers)
	drivers := fetchOpenF1Data("drivers?session_key=" + sessionKey)
	pilotos := make([]bson.M, 0, len(drivers))
	for _, d := range drivers {
		// Convertir numero a int (OpenF1 lo devuelve como float64)
		num := 0
		if v, ok := d["driver_number"].(float64); ok {
			num = int(v)
		}
		pilotos = append(pilotos, bson.M{
			"numero": num,
			"codigo": d["name_acronym"],
			"nombre": d["full_name"],
			"equipo": d["team_name"],
			"color":  d["team_colour"],
		})
	}

	// 4. Descargar stints (para mapear t_carrera_ms)
	stints := fetchOpenF1Data("stints?session_key=" + sessionKey)

	// 5. Definir endpoints
	type endpoint struct {
		Name   string
		Tipo   string
		URL    string
		IsLaps bool
	}
	endpoints := []endpoint{
		{"Posiciones", "posicion", "position?session_key=" + sessionKey, false},
		{"Vueltas", "vuelta", "laps?session_key=" + sessionKey, true},
		{"Pits", "pit", "pit?session_key=" + sessionKey, false},
		{"Clima", "clima", "weather?session_key=" + sessionKey, false},
		{"Ubicaciones", "ubicacion", "location?session_key=" + sessionKey, false},
		{"Intervalos", "intervalo", "intervals?session_key=" + sessionKey, false},
		{"Control de Carrera", "control", "race_control?session_key=" + sessionKey, false},
	}

	// Mapa para guardar t_carrera_ms de cada vuelta (lap_number -> t_carrera_ms)
	lapTimes := make(map[int]int64)

	// 6. Descargar y guardar cada endpoint
	for _, ep := range endpoints {
		fmt.Printf("⬇️ Descargando %s...\n", ep.Name)
		data := fetchOpenF1Data(ep.URL)
		if len(data) == 0 {
			fmt.Printf("⚠️ No se encontraron datos para %s\n", ep.Name)
			continue
		}

		var docs []interface{}
		omitidos := 0
		for _, item := range data {
			var dateStr string
			if ep.IsLaps {
				// En laps usar date_start
				if d, ok := item["date_start"].(string); ok {
					dateStr = d
				}
			} else {
				// En otros usar date
				if d, ok := item["date"].(string); ok {
					dateStr = d
				}
			}

			// Saltar si no hay fecha
			if dateStr == "" {
				omitidos++
				continue
			}

			timestamp, err := time.Parse(time.RFC3339, dateStr)
			if err != nil {
				omitidos++
				continue
			}

			tCarreraMs := timestamp.Sub(horaInicio).Milliseconds()

			// Para laps, guardar el t_carrera_ms de cada vuelta
			if ep.IsLaps {
				if lapNum, ok := item["lap_number"].(float64); ok {
					lapTimes[int(lapNum)] = tCarreraMs
				}
			}

			// Documento con t_carrera_ms en la raíz
			doc := bson.M{
				"carrera_id":   sessionKey,
				"timestamp":    timestamp,
				"t_carrera_ms": tCarreraMs,
				"tipo":         ep.Tipo,
				"datos": bson.M{
					"raw": item,
				},
			}
			docs = append(docs, doc)
		}

		if omitidos > 0 {
			fmt.Printf("⚠️ %s: %d registros omitidos por falta de fecha\n", ep.Name, omitidos)
		}

		// Insertar en bloques no ordenados
		if len(docs) > 0 {
			batchSize := 1000
			for i := 0; i < len(docs); i += batchSize {
				end := i + batchSize
				if end > len(docs) {
					end = len(docs)
				}
				batch := docs[i:end]
				_, err := db.Collection("eventos_carrera").InsertMany(ctx, batch, options.InsertMany().SetOrdered(false))
				if err != nil {
					log.Printf("❌ Error insertando bloque de %s: %v", ep.Name, err)
				}
			}
			fmt.Printf("✅ %s: %d documentos guardados.\n", ep.Name, len(docs))
		}
	}

	// 7. Insertar stints con t_carrera_ms calculado desde lapTimes
	if len(stints) > 0 {
		fmt.Printf("⬇️ Procesando Stints...\n")
		var docs []interface{}
		for _, s := range stints {
			lapStart, ok := s["lap_start"].(float64)
			if !ok {
				continue
			}
			tCarreraMs, ok := lapTimes[int(lapStart)]
			if !ok {
				continue
			}
			doc := bson.M{
				"carrera_id":   sessionKey,
				"timestamp":    horaInicio.Add(time.Duration(tCarreraMs) * time.Millisecond),
				"t_carrera_ms": tCarreraMs,
				"tipo":         "stint",
				"datos":        bson.M{"raw": s},
			}
			docs = append(docs, doc)
		}
		if len(docs) > 0 {
			_, err := db.Collection("eventos_carrera").InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
			if err != nil {
				log.Printf("❌ Error insertando stints: %v", err)
			} else {
				fmt.Printf("✅ Stints: %d documentos guardados.\n", len(docs))
			}
		}
	}

	// 8. Escribir documento de la carrera en 'carreras'
	totalVueltas := 0
	for lap := range lapTimes {
		if lap > totalVueltas {
			totalVueltas = lap
		}
	}
	// Convertir temporada a int
	temporada := 0
	if v, ok := session["year"].(float64); ok {
		temporada = int(v)
	}
	carreraDoc := bson.M{
		"carrera_id":    sessionKey,
		"nombre":        session["session_name"],
		"circuito":      session["circuit_short_name"],
		"temporada":     temporada,
		"fuente":        "openf1",
		"session_key":   sessionKey,
		"hora_largada":  horaInicio,
		"total_vueltas": totalVueltas,
		"pilotos":       pilotos,
		"creada_en":     time.Now(),
		"estado":        "descargada",
	}
	_, err = db.Collection("carreras").InsertOne(ctx, carreraDoc)
	if err != nil {
		log.Printf("❌ Error insertando carrera: %v", err)
	} else {
		fmt.Println("✅ Documento de carrera guardado en 'carreras'")
	}

	fmt.Println("🎉 ¡Carrera descargada exitosamente!")
}

func fetchOpenF1Data(endpoint string) []OpenF1Record {
	url := "https://api.openf1.org/v1/" + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creando petición HTTP: %v", err)
		return nil
	}

	// Authorization solo si existe la API key
	apiKey := os.Getenv("OPENF1_API_KEY")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := http.Client{Timeout: 60 * time.Second}
	var resp *http.Response
	var lastErr error

	// Reintentos con espera creciente
	for i := 0; i < 5; i++ {
		resp, lastErr = client.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		espera := time.Duration(1<<i) * time.Second
		log.Printf("Reintentando %s en %v (intento %d): %v", url, espera, i+1, lastErr)
		time.Sleep(espera)
	}

	if lastErr != nil {
		log.Printf("Error en petición HTTP a %s tras reintentos: %v", url, lastErr)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("API OpenF1 devolvió status %d para %s", resp.StatusCode, url)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error leyendo cuerpo de respuesta: %v", err)
		return nil
	}

	var records []OpenF1Record
	if err := json.Unmarshal(body, &records); err != nil {
		log.Printf("Error parseando JSON de %s: %v", url, err)
		return nil
	}
	return records
}
