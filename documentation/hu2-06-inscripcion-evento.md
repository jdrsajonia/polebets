# HU2-06 — Inscripción al evento multijugador

Parte de **Pagos** lista. Falta conectarla en el **Orquestador** (T2-02) y el **Front** cuando existan.

## Reglas (acordadas para el prototipo 2)

| Regla | Valor tentativo |
| --- | --- |
| Inscripción, en PoleCoins, va al botín | 100 (mínimo) |
| Fichas de juego por inscrito (no son PoleCoins) | 1000 |
| Comisión de la casa sobre el botín | 10 % |
| Fichas que hay que apostar para cobrar del botín | 50 % |
| Inscritos mínimos para que haya multijugador | 4 |

- Una sola inscripción por jugador y evento; los reintentos no cobran doble.
- Solo se inscribe antes de la largada. La largada la marca el Motor con el **tiempo de carrera**.
- Al largar con menos de 4 inscritos se devuelve la inscripción a todos y el evento queda solo individual.
- Los valores se cambian con `EVENT_ENTRY_FEE`, `EVENT_INITIAL_CHIPS`, `EVENT_COMMISSION_PCT`, `EVENT_MIN_WAGER_PCT` y `EVENT_MIN_PLAYERS`, o el Motor los manda al crear el evento.

## Flujo

1. El admin arranca la simulación → el **Motor** crea el evento en Pagos (`POST /internal/events`).
2. El jugador se inscribe en el Front → Gateway → **Orquestador** → Pagos (`POST /internal/events/{id}/registrations`). Pagos cobra, suma al botín y anota en el ledger, todo en una transacción.
3. Largada (tiempo de carrera) → el **Motor** llama `POST /internal/events/{id}/start`. Pagos cierra inscripciones y, si faltan jugadores, devuelve todo. La respuesta trae `players`: el Motor les crea las fichas en **Redis** (HU2-11, Guillermo).
4. Al apostar en vivo, el **Orquestador** consulta `GET /internal/events/{id}/registrations/{user_id}`: inscrito activo → multijugador (fichas); si no → individual (PoleCoins). Las estáticas siempre son individuales.

## API interna de Pagos

Todas exigen `X-Internal-Service-Token`.

| Método y ruta | Quién | Respuestas |
| --- | --- | --- |
| `POST /internal/events` `{event_id, name, race_ref, entry_fee?, initial_chips?, commission_pct?, min_wager_pct?, min_players?}` | Motor | 201 creado · 200 ya existía igual · 409 mismo id con otra configuración |
| `GET /internal/events/{id}` | Todos | 200 `{status, pot, registered, …}` · 404 |
| `POST /internal/events/{id}/registrations` `{user_id}` + `Idempotency-Key: event-entry:{id}:{user_id}` | Orquestador | 201 inscrito `{fee, initial_chips, pot, saldo}` · 200 ya inscrito (no cobra) · 400 clave inválida · 403 no es jugador · 404 evento o usuario · 409 ya largó · 422 saldo insuficiente |
| `GET /internal/events/{id}/registrations/{user_id}` | Orquestador | 200 `{status: active\|refunded}` · 404 no inscrito |
| `POST /internal/events/{id}/start` | Motor | 200 `{status: multiplayer\|individual_only, pot, players, refunded, nueva}`; repetirla no devuelve dos veces |

Estados del evento: `registration` → `multiplayer` o `individual_only` → `finished` (lo pone HU2-12 al repartir el botín).

## Datos (PostgreSQL de Pagos)

- `events`: id, name, race_ref, status, entry_fee, initial_chips, commission_pct, min_wager_pct, min_players, pot, created_at, started_at.
- `event_registrations`: id, event_id → events, user_id, fee, status (`active`/`refunded`), created_at. Único por (event_id, user_id).
- `ledger_entries`: nuevos tipos `event_entry` y `event_refund`, y columna `event_id`.

Concurrencia: inscribir y largar bloquean la fila del evento (`SELECT … FOR UPDATE`), así nunca se cruzan y el botín no pierde sumas.

## Notas para el equipo

- **Juan Diego (T2-03):** el saldo solo se toca en `_debitar_saldo` y `_acreditar_saldo` (`app/services/eventos.py`). Al pasar a `wallets` solo cambian esas dos funciones, y `event_registrations.user_id` deja de ser llave foránea.
- **Daniel (T2-02):** el Orquestador expone al jugador, por ejemplo, `POST /eventos/{id}/inscripcion` (toma `user_id` del JWT) y llama a Pagos con la clave de arriba.
- **Guillermo (HU2-11/12):** las fichas y el ranking viven en Redis; `players` de la largada dice a quién crearle `initial_chips`. Para el reparto, el botín está en `events.pot` y la comisión en `commission_pct`.
- Como cambian tablas y restricciones, hay que recrear la base: `docker compose down -v`.
