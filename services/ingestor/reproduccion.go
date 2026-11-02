package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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
