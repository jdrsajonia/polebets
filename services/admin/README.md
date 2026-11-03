# Admin (Python / FastAPI) — HU2-18, HU2-19 y HU2-23

Servicio que atiende a la CLI del administrador. Es el **único servicio de lógica
publicado al host** (puerto **8003**). No toca dinero ni apuestas: habla con Pagos y
con el Ingestor **solo por el Broker** (RabbitMQ), en petición-respuesta.

Cada petición declara una cola temporal exclusiva, la manda en `reply_to` junto con un
`correlation_id` (propiedades AMQP) y espera su respuesta hasta **10 s** (RF-30). Si no
llega, responde **504** «la consulta tardó demasiado». El mensaje expira a los 10 s, así
una orden vieja no se procesa después de que la CLI recibió el aviso. Si el Broker no
está, responde **503**.

`RABBITMQ_URL` es obligatoria: sin ella Admin no arranca.

## Rutas

| Ruta | Cola | Respuestas |
| --- | --- | --- |
| `GET /carreras` | `ingestor.consultas` (`list_races`) | 200 con las carreras ya descargadas en MongoDB |
| `POST /simulacion` | `ingestor.commands` (`start_simulation`) | 200 `{simulacion_id, hora_largada}`, 409 si ya hay una programada o en curso, 404 si la carrera no está descargada |
| `POST /consultas` | `admin.consultas` (consumidor de Pagos) | 200 con `data`, `filtros_aplicados` y `filtros_no_aplicados` |

```bash
curl localhost:8003/carreras
curl -X POST localhost:8003/simulacion -H 'Content-Type: application/json' \
  -d '{"race_id":"9165","minutos_anticipacion":10}'
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"rentabilidad","params":{"tipo_apuesta":"estatica","top_n":5}}'
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"resumen_financiero","params":{"modo":"torneo"}}'
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"participacion","params":{}}'
```

Consultas (`query`): `rentabilidad` (HU2-21), `resumen_financiero` (HU2-20),
`horarios` y `participacion` (HU2-22).

## Filtros (HU2-23, RF-29)

Se traducen al contrato de Pagos (`services/pagos/app/schemas/consultas.py`, que rechaza
campos extra):

| Admin (`params`) | Pagos (`filtros`) |
| --- | --- |
| `evento_id` (UUID) | `event_id` |
| `fecha_inicio` | `desde` |
| `fecha_fin` | `hasta` |
| `tipo_apuesta` (`estatica` \| `dinamica`) | `kind` |
| `plantilla` | `template` |
| `top_n` (1–100) | `top_n` |
| `modo` (`torneo` \| `individual`) | `modo` |

Cada consulta usa solo los filtros que le aplican; los demás no se mandan a Pagos
y vuelven en `filtros_no_aplicados`:

| Consulta | Filtros que aplica |
| --- | --- |
| `rentabilidad` | todos menos `modo` |
| `resumen_financiero`, `horarios`, `participacion` | `evento_id`, `fecha_inicio`, `fecha_fin`, `tipo_apuesta`, `modo` |

Pendiente: el login de admin contra Usuarios (RF-21) va con la CLI (HU2-17).
