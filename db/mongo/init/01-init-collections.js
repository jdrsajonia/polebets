// ============================================================
// 01-init-collections.js
// Inicialización de la base de datos MongoDB para el sistema
//
// Este script corre UNA SOLA VEZ, cuando el contenedor de Mongo
// arranca por primera vez. Docker lo ejecuta automáticamente
// porque está montado en /docker-entrypoint-initdb.d
// ============================================================

// Misma base que MONGO_DB del .env (el compose la pasa como MONGO_INITDB_DATABASE).
const nombreDB = process.env.MONGO_INITDB_DATABASE || 'apuestas_db';
db = db.getSiblingDB(nombreDB);

print('⏳ Inicializando base de datos: ' + nombreDB);

// ============================================================
// 1. COLECCIÓN: carreras
// Metadata de cada carrera (histórica o en vivo) que el motor
// va a reproducir o consumir desde OpenF1.
// ============================================================
db.createCollection('carreras', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: ['carrera_id', 'nombre', 'fuente', 'creada_en'],
      properties: {
        carrera_id: { bsonType: 'string' },
        nombre: { bsonType: 'string' },
        temporada: { bsonType: 'int' },
        circuito: { bsonType: 'string' },
        fuente: { enum: ['openf1', 'historica', 'simulada'] },
        estado: { enum: ['cargada', 'en_vivo', 'finalizada', 'descargada'] },
        session_key: { bsonType: 'string' },
        hora_largada: { bsonType: 'date' },
        total_vueltas: { bsonType: 'int' },
        pilotos: {
          bsonType: 'array',
          items: {
            bsonType: 'object',
            properties: {
              numero: { bsonType: 'int' },
              codigo: { bsonType: 'string' },
              nombre: { bsonType: 'string' },
              equipo: { bsonType: 'string' },
              color: { bsonType: 'string' }
            }
          }
        },
        creada_en: { bsonType: 'date' }
      }
    }
  }
});

db.carreras.createIndex({ carrera_id: 1 }, { unique: true });
db.carreras.createIndex({ estado: 1 });

print('✅ Colección "carreras" creada');


// ============================================================
// 2. COLECCIÓN: eventos_carrera
// Datos crudos normalizados de OpenF1.
// ============================================================
db.createCollection('eventos_carrera', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: ['carrera_id', 'timestamp', 'tipo', 't_carrera_ms', 'datos'],
      properties: {
        carrera_id: { bsonType: 'string' },
        timestamp: { bsonType: 'date' },
        // Milisegundos desde la largada (negativo antes). En la raíz para indexarlo.
        t_carrera_ms: { bsonType: 'long' },
        tipo: {
          enum: ['posicion', 'intervalo', 'vuelta', 'pit', 'stint', 'control', 'clima', 'ubicacion'],
          description: '8 tipos permitidos'
        },
        datos: { bsonType: 'object' },
        leido_en: { bsonType: 'date' },
        // Hora real en que la reproducción emitió el dato (lo lee el Motor).
        emitido_en: { bsonType: 'date' },
        simulacion_id: { bsonType: 'string' },
        // Descarga que escribió el dato: al volver a descargar se reemplaza
        // la carga anterior solo cuando la nueva terminó bien.
        carga_id: { bsonType: 'string' }
      }
    }
  }
});

db.eventos_carrera.createIndex({ carrera_id: 1, t_carrera_ms: 1 });
db.eventos_carrera.createIndex({ carrera_id: 1, tipo: 1, timestamp: 1 });
db.eventos_carrera.createIndex({ carrera_id: 1, tipo: 1, emitido_en: 1 });
db.eventos_carrera.createIndex({ carga_id: 1 });

print('✅ Colección "eventos_carrera" creada con índices');


// ============================================================
// 3. COLECCIÓN: simulaciones
// Nueva colección para HU2-19 (Iniciar simulación).
// ============================================================
db.createCollection('simulaciones', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: ['_id', 'carrera_id', 'estado', 'hora_largada', 'creada_en'],
      properties: {
        _id: { bsonType: 'string' }, // UUID
        carrera_id: { bsonType: 'string' },
        estado: { enum: ['programada', 'en_curso', 'finalizada', 'fallida'] },
        hora_largada: { bsonType: 'date' },
        iniciada_en: { bsonType: 'date' },
        terminada_en: { bsonType: 'date' },
        t_inicio_ms: { bsonType: 'long' },
        t_carrera_ms: { bsonType: 'long' },
        vuelta_actual: { bsonType: 'int' },
        total_vueltas: { bsonType: 'int' },
        latido_en: { bsonType: 'date' },
        motivo_fallo: { bsonType: 'string' },
        creada_en: { bsonType: 'date' }
      }
    }
  }
});
db.simulaciones.createIndex({ estado: 1, hora_largada: 1 });

print('✅ Colección "simulaciones" creada con índices');


// ============================================================
// 4. COLECCIÓN: mercados
// (Sin cambios)
// ============================================================
db.createCollection('mercados', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: [
        'carrera_id', 'tipo', 'pregunta', 'opciones', 'estado',
        'abre_en', 'cierra_en', 'creado_en'
      ],
      properties: {
        carrera_id: { bsonType: 'string' },
        tipo: { enum: ['cambio_posicion'] },
        pregunta: { bsonType: 'string' },
        opciones: { bsonType: 'array', minItems: 2, items: { bsonType: 'string' } },
        estado: { enum: ['abierto', 'cerrado', 'liquidado'] },
        abre_en: { bsonType: 'date' },
        cierra_en: { bsonType: 'date' },
        observa_hasta: { bsonType: 'date' },
        resultado: { bsonType: ['string', 'null'] },
        puntos_otorgados: { bsonType: ['int', 'double', 'null'] },
        liquidacion_error: { bsonType: 'string' },
        liquidacion_reintento_en: { bsonType: 'date' },
        liquidacion_lease: { bsonType: 'string' },
        liquidacion_lease_hasta: { bsonType: 'date' },
        creado_por: { bsonType: 'string' },
        creado_en: { bsonType: 'date' },
        liquidado_en: { bsonType: ['date', 'null'] }
      }
    }
  }
});

db.mercados.createIndex({ estado: 1, cierra_en: 1 });
db.mercados.createIndex({ estado: 1, observa_hasta: 1, liquidacion_reintento_en: 1 });
db.mercados.createIndex({ carrera_id: 1, creado_en: -1 });

print('✅ Colección "mercados" creada con índices');


// ============================================================
// 5. COLECCIÓN: apuestas_contexto
// (Sin cambios)
// ============================================================
db.createCollection('apuestas_contexto', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: [
        'apuesta_id', 'mercado_id', 'usuario_id',
        'opcion', 'creada_en'
      ],
      properties: {
        apuesta_id: { bsonType: 'string' },
        mercado_id: { bsonType: 'objectId' },
        usuario_id: { bsonType: 'string' },
        opcion: { bsonType: 'string' },
        monto: { bsonType: 'int', minimum: 1 },
        snapshot_evento: { bsonType: 'object' },
        creada_en: { bsonType: 'date' },
        liquidada: { bsonType: 'bool' },
        gano: { bsonType: ['bool', 'null'] },
        puntos_obtenidos: { bsonType: ['double', 'int'], minimum: 0 }
      }
    }
  }
});

db.apuestas_contexto.createIndex({ apuesta_id: 1 }, { unique: true });
db.apuestas_contexto.createIndex({ mercado_id: 1, liquidada: 1 });
db.apuestas_contexto.createIndex({ usuario_id: 1, creada_en: -1 });

print('✅ Colección "apuestas_contexto" creada con índices');


// ============================================================
// 6. COLECCIÓN: reproducciones
// (Sin cambios, para los workers viejos del Motor)
// ============================================================
db.createCollection('reproducciones', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: ['_id', 'iniciada_en', 'latido_en'],
      properties: {
        _id: { bsonType: 'string' },
        iniciada_en: { bsonType: 'date' },
        latido_en: { bsonType: 'date' },
        terminada_en: { bsonType: ['date', 'null'] },
        desde: { bsonType: 'date' },
        factor: { bsonType: 'double' }
      }
    }
  }
});

print('✅ Colección "reproducciones" creada');


// ============================================================
// RESUMEN FINAL
// ============================================================
print('');
print('========================================');
print('✅ Inicialización de apuestas_db completa');
print('========================================');
print('Colecciones creadas:');
print('  1. carreras             (metadata de carreras)');
print('  2. eventos_carrera      (datos crudos de OpenF1)');
print('  3. simulaciones         (HU2-19: simulación programada)');
print('  4. mercados             (mercados del admin)');
print('  5. apuestas_contexto    (contexto de apuestas del motor)');
print('  6. reproducciones       (latido de la reproducción, HU-07)');
print('');
print('Índices creados: 13');
print('Validaciones: 6 (una por colección)');
print('========================================');