# HU2-22 · Horarios y participación por modo

Como administrador, quiero saber cuándo y cómo juega la gente, para decidir en qué momentos conviene abrir mercados (RF-27, RF-28).

Son dos consultas nuevas por el mismo camino que la rentabilidad (HU2-21) y el resumen financiero (HU2-20): la CLI llama a Admin (`POST /consultas`), Admin la publica en `admin.consultas` y el consumidor de Pagos responde por `reply_to` con el mismo `correlation_id`.

## `horarios` (RF-27)

Ganancia y volumen apostado de las apuestas **individuales** (`bets`), agrupados de dos formas:

| Grupo | Clave |
| --- | --- |
| `por_hora` | franja de 1 hora (0–23) de `bets.created_at`, en **hora de Colombia** (`America/Bogota`; las fechas se guardan en UTC) |
| `por_vuelta` | `bets.lap`; `vuelta: null` agrupa las apuestas sin vuelta |

- `volumen` = suma de `amount` (incluye las apuestas sin resolver).
- `ganancia` = suma de `amount − payout`, con `payout` nulo como 0. Es la misma definición que el resumen financiero (`debit − settle`).

```json
{ "tipo": "horarios", "ok": true,
  "datos": { "por_hora":   [ { "hora": 21, "volumen": 40, "ganancia": 40 } ],
             "por_vuelta": [ { "vuelta": null, "volumen": 40, "ganancia": 40 } ] } }
```

## `participacion` (RF-28)

Una fila por evento, del más reciente al más viejo:

| Campo | Qué es |
| --- | --- |
| `event_id`, `race_key`, `starts_at`, `tournament_status` | identifican el evento y dicen si el torneo ya se repartió |
| `inscritos` | filas de `event_entries`; incluye las de un torneo cancelado, como el calendario (`orquestador-api.md`) |
| `solo_individuales` | jugadores con apuestas individuales en el evento que no están inscritos |
| `botin` | `events.pot` (0 si el torneo se canceló) |
| `ganancia_torneo`, `ganancia_individual`, `apostado_pendiente` | del resumen financiero del evento (HU2-20), por modo |

Mientras el torneo no esté `repartido`, `ganancia_torneo` todavía incluye el botín: por eso cada fila lleva `tournament_status`. Igual con lo individual y `apostado_pendiente`.

## Filtros (RF-29)

Los mismos del resumen financiero (`FiltrosResumen`): `plantilla` y `top_n` no aplican y Admin los devuelve en `filtros_no_aplicados`.

| Filtro | `horarios` | `participacion` |
| --- | --- | --- |
| `evento_id` | solo ese evento | solo ese evento |
| `fecha_inicio` / `fecha_fin` | fecha de la apuesta | eligen los eventos por su largada (`starts_at`); cada evento se muestra completo, porque RF-28 es por evento |
| `modo` | con `torneo` sale vacío (ver límites) | deja solo los datos de ese modo; los del otro salen en 0 |
| `tipo_apuesta` | por el mercado de la apuesta | aplica a la parte individual; la del torneo sale en 0 |

Sin datos: listas vacías. Filtros inválidos: `{"ok": false, "error": "filtros_invalidos"}`.

## Límites conocidos (fuera de esta HU)

1. **`por_vuelta` sale con todo en `vuelta: null`.** El Motor no asigna `lap` al débito que manda a Pagos (`services/motor/internal/apostar/apostar.go`), aunque RF-27 pide que cada apuesta guarde la vuelta en que se hizo. Es de HU2-09; la consulta ya agrupa por vuelta en cuanto llegue el dato.
2. **Los horarios solo cubren apuestas individuales.** Las de torneo se guardan solo en Redis y no llegan a `bets` de Pagos (RF-12 y RNF-20 piden que Pagos las registre). También es de HU2-09.

## Archivos

- `services/pagos/app/services/horarios.py` y `services/pagos/app/services/participacion.py`: las consultas.
- `services/pagos/app/schemas/consultas.py`: los tipos `horarios` y `participacion` y sus respuestas.
- `services/pagos/app/services/consulta_handler.py`: los dos casos nuevos (comparten la validación de filtros con el resumen).
- `services/admin/app/main.py`: los dos tipos en `query` y en `_FILTROS_POR_CONSULTA`.
- `services/pagos/tests/test_horarios.py` y `services/pagos/tests/test_participacion.py`.

## Probar a mano

```bash
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"horarios","params":{}}'
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"participacion","params":{"modo":"torneo"}}'
```
