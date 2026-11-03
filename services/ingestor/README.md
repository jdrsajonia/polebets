# Ingestor (Go) — HU2-16 y HU2-19

Único componente que habla con **OpenF1** y único que **escribe en MongoDB** (RNF-18).
Hace dos cosas:

1. **Descargar** una sesión de OpenF1, una sola vez, con un comando (RF-23).
2. **Servicio**: recibe por el Broker la orden de simulación y la lista de carreras
   (RF-22), y a la hora de largada **reproduce** la carrera según su tiempo de carrera.

> ⚠️ Este PR cambia los validadores de Mongo (`db/mongo/init/01-init-collections.js`)
> y elimina `02-seed-data.js` (la carrera de prueba `2024_monza` del P1: la carrera
> real ahora sale de `descargar`). Los scripts de init solo corren con el volumen
> vacío, así que hay que recrearlo:
>
> ```bash
> docker compose down -v && docker compose up --build -d
> ```

## Archivos

| Archivo | Responsabilidad |
| --- | --- |
| `main.go` | Arma las piezas: config, conexión a Mongo y el comando (`servicio` o `descargar`) |
| `config.go` | Variables de entorno (`MONGO_URI`, `MONGO_DB`, `RABBITMQ_URL` obligatorias) |
| `openf1.go` | Cliente de OpenF1: reintentos con espera creciente ante 429/5xx, `Retry-After`, 404 = sin datos |
| `descarga.go` | Comando `descargar`: baja la sesión y la guarda en `eventos_carrera` y `carreras` |
| `normalizar.go` | Funciones puras de la descarga: `t_carrera_ms`, vueltas, stints, ventanas, pilotos |
| `broker.go` | Consumidor de RabbitMQ (`ingestor.commands` e `ingestor.consultas`) y healthcheck |
| `simulaciones.go` | Programar una simulación (una a la vez) y listar las carreras descargadas |
| `reproduccion.go` | Monitor de simulaciones y bucle de reproducción |
| `reloj.go` | Funciones puras del reloj de carrera, el rango de cada tick y `vuelta_actual` |

## Descargar una carrera

```bash
docker exec -it arqui_ingestor /app/ingestor descargar <session_key>
# ej.: 9165 = Singapur 2023, carrera. 9158 es la Práctica 1 (OpenF1 no trae /intervals de prácticas).
```

En Git Bash de Windows, antepón `MSYS_NO_PATHCONV=1` para que no reescriba `/app/...`.

- Endpoints: `/sessions`, `/drivers`, `/laps`, `/stints`, `/pit`, `/weather`, `/race_control`,
  `/position`, `/intervals` y `/location`.
- **Por tramos** (HU2-16): `/position` e `/intervals` se piden por piloto; `/location`
  (el pesado, para el visor) por piloto y en ventanas de 10 min, desde la largada
  hasta el fin de la sesión + 30 min. OpenF1 sin token permite ~30 peticiones/min:
  cuando responde 429 se espera lo que pide `Retry-After` (60 s). Una carrera tarda
  ~15–20 min y una práctica ~9 min.
- Cada documento de `eventos_carrera` lleva `tipo` (`posicion, intervalo, vuelta, pit,
  stint, control, clima, ubicacion`), `t_carrera_ms` en la raíz (ms desde la largada,
  negativo antes) y el registro de OpenF1 tal cual en `datos.raw`.
- Los registros sin fecha se saltan y se cuentan en el log. La vuelta 1 sin
  `date_start` arranca en `t = 0`; los stints toman el `t_carrera_ms` de su vuelta
  `lap_start` (la del líder).
- **Idempotente**: lo nuevo se escribe con un `carga_id` y, solo si todo salió bien,
  se borran los eventos de cargas anteriores y se reemplaza el documento de
  `carreras`. Si OpenF1 falla tras los reintentos, se borra lo nuevo, la carrera
  anterior queda intacta y el comando sale con código ≠ 0. Si el proceso muere sin
  limpiar (p. ej. `docker restart` en plena descarga), los eventos huérfanos no se
  reproducen (no hay documento en `carreras`) y los borra la siguiente descarga buena.
- No se puede volver a descargar una carrera con una simulación programada o en curso.

## Servicio y reproducción

`ENTRYPOINT` del contenedor: `/app/ingestor servicio`.

| Cola | Mensaje (cuerpo) | Respuesta por `reply_to` |
| --- | --- | --- |
| `ingestor.commands` | `{"action":"start_simulation","race_id":"9165","minutos_anticipacion":10}` | `{"ok":true,"simulacion_id","hora_largada"}` o `{"ok":false,"error":"ya_hay_simulacion" \| "carrera_no_descargada" \| "orden_invalida" \| "error_interno"}` |
| `ingestor.consultas` | `{"action":"list_races"}` | `{"ok":true,"carreras":[{carrera_id, nombre, circuito, temporada, total_vueltas, hora_largada, estado, pilotos}]}` |

`correlation_id` y `reply_to` van en las propiedades AMQP. Los mensajes se procesan de
a uno y se confirman (ack) después de responder.

- La orden crea en `simulaciones` un documento `programada` con
  `hora_largada = ahora + minutos_anticipacion` y `t_inicio_ms = 0`. Se rechaza si ya
  hay una `programada` o `en_curso` (RF-22).
- Cada segundo, el monitor pasa la siguiente simulación vencida de `programada` a
  `en_curso` con `iniciada_en` en una sola operación (`FindOneAndUpdate`).
- **Reloj**: `t = t_inicio_ms + (ahora − iniciada_en)`. Cada 200 ms se marca
  `emitido_en` y `simulacion_id` en los eventos con `t_carrera_ms` en `(t_anterior, t]`
  (el primer tick emite todo lo previo a la largada). Sin offsets aleatorios: la misma
  carrera con el mismo `t_inicio_ms` da la misma secuencia (RNF-20).
- Cada segundo se actualizan en la simulación `t_carrera_ms`, `vuelta_actual` (la vuelta
  más alta empezada, la del líder), `total_vueltas` y `latido_en`, y `latido_en` en
  `reproducciones` (la leen los workers del P1 que siguen en el Motor).
- Al pasar el último dato: `finalizada` con `terminada_en`. Si el Ingestor se detiene o
  se reinicia con una en curso, queda `fallida` con `motivo_fallo` (RNF-16); si Mongo
  falla más de 30 s seguidos, también.

## Probar con Admin (puerto 8003)

```bash
curl localhost:8003/carreras
curl -X POST localhost:8003/simulacion -H 'Content-Type: application/json' \
  -d '{"race_id":"9165","minutos_anticipacion":1}'      # 200; otra vez → 409; carrera no descargada → 404
```

Seguir la simulación en Mongo:

```bash
docker exec -it arqui_mongo mongosh -u arqui -p arqui_pass --authenticationDatabase admin apuestas_db \
  --eval 'db.simulaciones.find({}, {estado:1, t_carrera_ms:1, vuelta_actual:1, total_vueltas:1, latido_en:1})'
```

## Tests

```bash
gofmt -l . && go vet ./... && go test ./...
```

Cubren el reloj (incluido el bug de `iniciada_en` reiniciada en cada tick), el rango de
cada tick, `vuelta_actual`, el cálculo de `t_carrera_ms`, los stints de la vuelta 1, los
pilotos con nulos y el rechazo de una segunda simulación.
