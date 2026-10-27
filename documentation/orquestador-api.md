# Orquestador — contrato del API (T2-02)

Contrato del Orquestador del prototipo 2: qué rutas recibe del API Gateway, qué responde y a qué servicio llama en cada paso. Lo acuerdan Daniel (T2-02), Michael (HU2-06, HU2-09) y Misael (HU2-01, HU2-03). Los nombres de tablas, estados y reglas salen del documento «PoleBet — Prototipo 2»; los valores entre corchetes son los tentativos de ese documento.

## 1. Qué es y qué no hace

El Orquestador (Go, puerto interno 8081) coordina los flujos que tocan varios servicios: registro, página del usuario (SSR), compra de tokens, calendario, inscripción y apuesta. Llama a Usuarios, Pagos y Motor en orden, responde un solo resultado y, si falla un paso intermedio, reintenta lo idempotente para no dejar el flujo a medias.

No hace:

- **No valida el JWT.** Eso lo hace el Gateway con `auth_request` contra Usuarios. El Orquestador confía en los headers que pone el Gateway.
- **No tiene base de datos** ni lee las de otros servicios (RNF-18). Todo dato lo pide por REST.
- **No lo llama ningún otro servicio.** Solo recibe tráfico del Gateway; Usuarios, Pagos y Motor nunca lo llaman.
- **No aplica reglas de dinero ni de apuestas.** Saldo, tope [500], mercado abierto, fichas e idempotencia del débito los decide quien es dueño del dato (Pagos o Motor). El Orquestador solo valida la forma del cuerpo y el rol.

## 2. Entrada

### Rutas y prefijo

Las rutas públicas llevan el prefijo `/api`, que pone el Gateway. En este documento se escriben con el prefijo (`/api/eventos`); el Gateway le pasa al Orquestador la ruta sin él (`/eventos`).

Solo dos rutas públicas no pasan por `auth_request`: `POST /api/registro` (Orquestador) y el login (Usuarios). Todas las demás exigen sesión.

### Headers del Gateway

| Header | Valor | Si falta o es inválido |
| --- | --- | --- |
| `X-User-Id` | uuid del usuario (`users.id`) | 401 `sin_sesion` |
| `X-User-Rol` | `player` o `admin` (los valores de `users.role`) | 401 `sin_sesion` |

- **Decisión: los valores de `X-User-Rol` son `player` y `admin`**, los mismos de `users.role`. «Rol jugador» en el documento del P2 es solo texto.
- Todas las rutas con sesión son de jugador: si `X-User-Rol` no es `player`, responde 403 `solo_jugadores`. El administrador usa la CLI, no la web. El rol se valida aquí y no en Pagos.
- **Requisito para el Gateway (Tomás):** borrar los `X-User-Id` y `X-User-Rol` que mande el cliente y poner solo los que devuelve `auth_request`. Sin eso cualquiera se haría pasar por otro jugador.

### Formato de error común

Todas las respuestas de error tienen el mismo cuerpo:

```json
{ "error": "Mercado cerrado", "codigo": "mercado_cerrado" }
```

- `error`: mensaje en español que el front puede mostrar tal cual. Es compatible con `services/front/src/api/http.ts`, que ya lee `error`.
- `codigo`: identificador estable en `snake_case` para que el front decida qué hacer sin comparar textos.

| HTTP | `codigo` | Cuándo |
| --- | --- | --- |
| 400 | `cuerpo_invalido` | JSON mal formado, campo faltante, de otro tipo o desconocido |
| 400 | `falta_idempotency_key` | Ruta que exige `Idempotency-Key` y no llegó (o pasa de 128 caracteres) |
| 401 | `sin_sesion` | Faltan o son inválidos los headers del Gateway |
| 403 | `solo_jugadores` | `X-User-Rol` distinto de `player` |
| 404 | `evento_no_existe` | El evento no existe |
| 404 | `mercado_no_existe` | El mercado no existe |
| 409 | `correo_registrado` | «Este correo ya está registrado» |
| 409 | `inscripciones_cerradas` | La carrera ya largó |
| 409 | `mercado_cerrado` | «Mercado cerrado» (RF-13) |
| 409 | `ya_apostaste` | Ya hay una apuesta del jugador en ese mercado (único `user_id`, `market_id`) |
| 409 | `clave_reutilizada` | La `Idempotency-Key` ya se usó con otro contenido |
| 422 | `saldo_insuficiente` | No alcanza el saldo en tokens |
| 422 | `fichas_insuficientes` | No alcanzan las fichas del torneo (incluye llegar a 0) |
| 422 | `monto_invalido` | Monto ≤ 0, o supera el máximo individual de [500] tokens, o monto de compra inválido |
| 422 | `opcion_invalida` | La opción no pertenece al mercado |
| 503 | `servicio_no_disponible` | Un servicio no respondió tras los reintentos |
| 500 | `error_interno` | Cualquier otro caso (se registra en el log) |

### Nombres de la API pública

En español y iguales en todos los flujos:

| Campo | Significado |
| --- | --- |
| `usuario` | `{id, nombre, email, rol}` (mismo objeto que el P1) |
| `saldo` | tokens en `wallets.balance` |
| `evento_id`, `mercado_id`, `apuesta_id` | ids de `events`, `markets` y `bets` |
| `largada` | `events.starts_at` (ISO 8601) |
| `estado` | `events.status`: `programado` \| `en_curso` \| `finalizado` |
| `estado_torneo` | `events.tournament_status`: `abierto` \| `activo` \| `cancelado` \| `repartido` |
| `carrera` | `events.race_key`: carrera descargada en MongoDB (ej. `2024_monza`); no es el `evento_id` |
| `inscritos`, `botin` | cantidad de filas del evento en `event_entries` (incluye las devueltas si el torneo se canceló) y `events.pot` |
| `devuelta` | la inscripción se devolvió: inscrito y `tournament_status = cancelado` |
| `costo_inscripcion`, `fichas_iniciales` | `events.buy_in` e `events.initial_chips` |
| `opcion`, `monto`, `multiplicador` | `bets.option`, `bets.amount`, `bets.multiplier` |
| `modo` | `bets.mode`: `individual` \| `torneo` |
| `fichas`, `puesto` | fichas actuales y puesto en el ranking del torneo |

## 3. Flujos

### 3.1 Registro (RF-01, HU2-01)

`POST /api/registro` — **sin sesión**.

```json
// Petición
{ "nombre": "Ana", "email": "ana@correo.co", "contrasena": "clave-segura" }

// 201
{ "usuario": { "id": "0b6c…", "nombre": "Ana", "email": "ana@correo.co", "rol": "player" } }
```

Errores: 400 `cuerpo_invalido` (también los errores de validación de Usuarios: correo mal formado, contraseña corta), 409 `correo_registrado`, 503 `servicio_no_disponible`.

No abre sesión: después del registro el front hace login (Gateway → Usuarios), igual que en el P1.

**Pasos**

| # | Llama a | Ruta | Espera |
| --- | --- | --- | --- |
| 0 | — | — | El Orquestador genera el `user_id` (uuid v4) |
| 1 | Pagos | `POST /internal/wallets {user_id}` · `Idempotency-Key: wallet:{user_id}` | 201 creada o 200 ya existía; saldo 0 |
| 2 | Usuarios | `POST /internal/users {id, nombre, email, contrasena}` | 201 con el usuario (rol `player`); 409 si el correo existe |

**Decisión: la billetera va primero.** Así nunca queda una cuenta sin billetera (RF-01): la cuenta solo existe si la billetera ya existe.

**Si falla**

- Paso 1: es idempotente; se reintenta con la misma clave. Si sigue fallando → 503 y no se crea nada en Usuarios.
- Paso 2: se reintenta con el mismo `id` (Usuarios debe responder 200 si el `id` ya existe con el mismo correo, ver §5). Si el correo está repetido → 409 `correo_registrado`; si sigue fallando → 503.
- **Sin compensación.** Si el paso 2 no termina, queda una billetera con saldo 0 de un `user_id` que nadie usa. Es inofensiva: nadie puede iniciar sesión con ese id ni acreditarle tokens.
- Esas billeteras no afectan el resumen financiero: HU2-20 sale del ledger y ellas nunca tienen movimientos.

### 3.2 Página del usuario SSR (RF-04, HU2-03)

`GET /api/pagina-usuario` — sesión de jugador. La llama el servidor SSR del Front reenviando la cookie del jugador; el Gateway la valida y agrega los headers.

```json
// 200
{
  "usuario": { "id": "0b6c…", "nombre": "Ana", "email": "ana@correo.co", "rol": "player" },
  "saldo": 1250,
  "eventos": [
    { "evento_id": "evt_7c2a…", "carrera": "2024_monza", "estado": "en_curso",
      "estado_torneo": "activo", "devuelta": false,
      "fichas": 1340, "puesto": 2, "premio": null }
  ],
  "apuestas_activas": [
    { "apuesta_id": "7f1e…", "mercado_id": "…", "evento_id": "evt_7c2a…",
      "opcion": "VER", "monto": 200, "modo": "individual", "multiplicador": 2.5 }
  ],
  "transacciones": [
    { "tipo": "purchase", "tokens": 50, "evento_id": null, "creada_en": "2026-10-26T15:02:11Z" }
  ],
  "avisos": []
}
```

`transacciones` son las últimas 10 de `ledger_entries`; `tipo` es `ledger_entries.type` (`purchase`, `debit`, `settle`, `buy_in`, `refund`, `prize`).

**Pasos** (todos en paralelo salvo el 6, que depende del 3)

| # | Llama a | Ruta | Para |
| --- | --- | --- | --- |
| 1 | Usuarios | `GET /internal/users/{user_id}` | `usuario` |
| 2 | Pagos | `GET /internal/wallets/{user_id}` | `saldo` |
| 3 | Pagos | `GET /internal/users/{user_id}/event-entries` | eventos donde está inscrito, con `status`, `tournament_status`, `chips_final`, `position_final`, `prize` |
| 4 | Pagos | `GET /internal/users/{user_id}/bets?status=pending` | `apuestas_activas` |
| 5 | Pagos | `GET /internal/users/{user_id}/ledger?limit=10` | `transacciones` |
| 6 | Motor | `GET /internal/torneos/{evento_id}/jugadores/{user_id}` | `fichas` y `puesto` en vivo (Redis), solo para eventos con `tournament_status = activo` |

Fichas y puesto: si el torneo está `activo` salen del Motor (Redis); si está `repartido` salen de `event_entries` (`chips_final`, `position_final`); si está `abierto` o `cancelado` van en `null`.

Torneo cancelado (RF-08): la fila de `event_entries` se queda como historial y el evento sigue en la lista con `devuelta: true`. El estado sale de `events.tournament_status = cancelado`, y la devolución se ve en `transacciones` como un movimiento `refund`.

**Si falla** (solo hay lecturas, así que todas se reintentan)

- Usuarios no responde → 503: sin perfil no hay página.
- Pagos no responde → 200 con `usuario`, las secciones de Pagos en `null` y un mensaje en `avisos` (por ejemplo «No pudimos cargar tu saldo»). La página no se rompe (HU2-03).
- Motor no responde → 200 con `fichas` y `puesto` en `null` en esos eventos y un aviso.

### 3.3 Compra de tokens (RF-05, HU2-04)

`GET /api/billetera` — sesión de jugador.

```json
// 200
{ "saldo": 1250, "cop_por_token": 1000 }
```

Pasos: Pagos `GET /internal/wallets/{user_id}` (trae `balance` y la tasa configurada). Lectura: se reintenta; si Pagos no responde → 503.

`POST /api/billetera/compras` — sesión de jugador, **`Idempotency-Key` obligatoria** (la genera el front una vez por compra y la reutiliza en los reintentos, RNF-12).

```json
// Petición
{ "monto_cop": 50000 }

// 201 compra nueva · 200 reenvío de una compra ya hecha (no acredita de nuevo)
{ "compra_id": 41, "tokens_acreditados": 50, "saldo": 1300, "creada_en": "2026-10-26T15:02:11Z" }
```

1 token (PoleCoin) = 1000 COP: con `cop_por_token` 1000, una compra de 50 000 COP acredita 50 tokens.

Errores: 400 `falta_idempotency_key`, 409 `clave_reutilizada`, 422 `monto_invalido`, 503.

Las reglas del monto (mayor que 0, conversión con la tasa, topes) no las decide el Orquestador: las aplica Pagos (HU2-04, Juan Diego) y el Orquestador solo traduce su 422 a `monto_invalido`.

**Pasos**

| # | Llama a | Ruta | Espera |
| --- | --- | --- | --- |
| 1 | Pagos | `POST /internal/purchases {user_id, monto_cop}` · `Idempotency-Key: purchase:{user_id}:{clave del front}` | 201 o 200 con el movimiento del ledger (`type = purchase`) y el saldo nuevo |

La clave lleva el `user_id` para que dos jugadores que generen la misma clave no choquen. Timeout o 5xx → reintento con la misma clave. 4xx de Pagos → se traduce y se responde sin reintentar.

### 3.4 Calendario de eventos (RF-06, HU2-05)

`GET /api/eventos` — sesión de jugador. Eventos con `status` `programado` o `en_curso`.

```json
// 200
{
  "eventos": [
    { "evento_id": "evt_7c2a…", "carrera": "2024_monza", "estado": "programado",
      "estado_torneo": "abierto", "largada": "2026-10-26T16:00:00Z", "inscritos": 3, "botin": 300,
      "costo_inscripcion": 100, "fichas_iniciales": 1000 }
  ]
}
```

`GET /api/eventos/{evento_id}` — lo mismo de un evento, más sus mercados estáticos y si el jugador está inscrito.

```json
// 200
{
  "evento": { "evento_id": "evt_7c2a…", "carrera": "2024_monza", "estado": "programado",
              "estado_torneo": "abierto", "largada": "2026-10-26T16:00:00Z", "inscritos": 3, "botin": 300,
              "costo_inscripcion": 100, "fichas_iniciales": 1000 },
  "mercados_estaticos": [
    { "mercado_id": "…", "plantilla": "ganador", "pregunta": "¿Quién gana la carrera?",
      "estado": "abierto", "cierra_en": "2026-10-26T16:00:00Z",
      "opciones": [ { "opcion": "VER", "multiplicador": 2.5 } ] }
  ],
  "mi_inscripcion": { "inscrito": true, "devuelta": false }
}
```

`mi_inscripcion.devuelta` es `true` cuando el jugador está inscrito y `tournament_status = cancelado` (RF-08): la fila de `event_entries` se queda como historial y la devolución está en el ledger como `refund`. `inscritos` cuenta todas las filas del evento en `event_entries`.

Errores: 404 `evento_no_existe`, 503.

**Pasos**

| Ruta pública | Llama a | Ruta interna |
| --- | --- | --- |
| `GET /api/eventos` | Pagos | `GET /internal/events?status=programado,en_curso` |
| `GET /api/eventos/{id}` | Pagos | `GET /internal/events/{id}` |
| | Pagos | `GET /internal/events/{id}/markets?kind=estatica` |
| | Pagos | `GET /internal/events/{id}/registrations/{user_id}` (404 = no inscrito) |

Las tres del detalle van en paralelo. Son lecturas: se reintentan; si Pagos no responde → 503.

**Decisión: no hay ruta de «vista previa» de la apuesta (RF-09).** Para mostrar el modo antes de confirmar, el front usa `mi_inscripcion` y `estado_torneo` de esta ruta, y el multiplicador del mercado que llega por WebSocket. Las estáticas siempre son individuales; en las dinámicas, inscrito con torneo `activo` = torneo, y si no, individual.

### 3.5 Inscripción al torneo (RF-07, HU2-06)

`POST /api/eventos/{evento_id}/inscripcion` — sesión de jugador, sin cuerpo. No necesita `Idempotency-Key` del front: la clave la arma el Orquestador y es la misma en cada intento.

```json
// 201 inscrito ahora · 200 ya estaba inscrito (no se cobra)
{ "evento_id": "evt_7c2a…", "ya_inscrito": false, "costo_inscripcion": 100,
  "fichas_iniciales": 1000, "botin": 400, "saldo": 1150 }
```

Errores: 404 `evento_no_existe`, 409 `inscripciones_cerradas`, 422 `saldo_insuficiente`, 503.

**Pasos**

| # | Llama a | Ruta | Espera |
| --- | --- | --- | --- |
| 1 | Pagos | `POST /internal/events/{evento_id}/registrations {user_id}` · `Idempotency-Key: event-entry:{evento_id}:{user_id}` | 201 inscrito (débito `buy_in` en el ledger, suma al `pot` y crea la fila en `event_entries`, todo en una transacción); 200 ya inscrito |

**Decisiones**

- **Un solo paso, sin llamada al Motor y sin compensación.** Las fichas en Redis se crean en la largada, con los `players` que devuelve `POST /internal/events/{id}/start`. Antes de la largada, el ranking muestra inscritos y botín desde Pagos.
- **Ya inscrito → 200 con `ya_inscrito: true` y sin cobro.** Con la clave determinista, un reintento y un segundo intento son lo mismo; no se cobra dos veces (RNF-12).
- Timeout o 5xx → reintento con la misma clave.

> **Nota:** RF-07 se ajusta: el inscrito entra al ranking en la largada (avisar a Guillermo).

### 3.6 Apuesta (RF-12, HU2-09)

`POST /api/apuestas` — sesión de jugador, **`Idempotency-Key` obligatoria** (una por apuesta, reutilizada en los reintentos; HU2-09 corrige el front que generaba una por clic).

```json
// Petición
{ "mercado_id": "…", "opcion": "si", "monto": 200 }

// 201 apuesta nueva · 200 reenvío con la misma clave (no se debita de nuevo)
{ "apuesta_id": "7f1e…", "mercado_id": "…", "opcion": "si", "monto": 200,
  "modo": "torneo", "multiplicador": 1.8, "pago_si_acierta": 360,
  "saldo": null, "fichas": 1140 }
```

En modo `individual` viene `saldo` (tokens que quedan) y `fichas` en `null`; en `torneo`, al revés. `pago_si_acierta` = monto × multiplicador, en tokens o fichas según el modo.

Errores: 400 `cuerpo_invalido` / `falta_idempotency_key`, 404 `mercado_no_existe`, 409 `mercado_cerrado` / `ya_apostaste` / `clave_reutilizada`, 422 `monto_invalido` / `opcion_invalida` / `saldo_insuficiente` / `fichas_insuficientes`, 503.

**Pasos**

| # | Llama a | Ruta | Espera |
| --- | --- | --- | --- |
| 1 | — | — | Valida headers, rol y forma del cuerpo (`monto` entero > 0) |
| 2 | Motor | `POST /internal/apuestas {user_id, mercado_id, opcion, monto}` · reenvía la `Idempotency-Key` del front | 201 o 200 con la apuesta, el modo y el saldo o las fichas |

**Decisión: el modo lo decide el Motor.** El Motor sabe si el jugador es inscrito activo porque tiene sus fichas en Redis (se crean en la largada; si el torneo se canceló no existen). El Orquestador no consulta a Pagos en este flujo.

Lo que hace el Motor (HU2-09, Michael): valida mercado abierto en tiempo de carrera (RF-13, RNF-19), opción, modo, tope de [500] tokens en individual y fichas suficientes en torneo; en individual pide a Pagos debitar y registrar en una sola transacción; en torneo escribe el delta en Redis y Pagos registra la apuesta. Deriva el `bet_id` del par (usuario, clave), como hoy en `services/motor/internal/apuestas/servicio.go`, así que la misma clave da la misma apuesta. Si un paso interno del Motor falla, lo deshace el Motor.

**Si falla:** timeout o 5xx del Motor → el Orquestador reintenta con la misma clave. 4xx → se traduce a la tabla de errores común sin reintentar.

## 4. Llamadas salientes

### Configuración

Variables de entorno. Todas son obligatorias y sin valor por defecto (el servicio no arranca si falta una), salvo `HTTP_PORT`, que es opcional como en el motor.

| Variable | Uso |
| --- | --- |
| `HTTP_PORT` | Puerto de escucha; opcional, por defecto 8081 |
| `USUARIOS_BASE_URL` | `http://usuarios:8001` en Compose |
| `PAGOS_BASE_URL` | `http://pagos:8000` |
| `MOTOR_BASE_URL` | `http://motor:8080` |
| `INTERNAL_API_TOKEN` | Token de servicio, mínimo 32 caracteres (mismo nombre que usa el motor) |

### Headers en cada llamada

| Header | Valor |
| --- | --- |
| `X-Internal-Service-Token` | `INTERNAL_API_TOKEN` |
| `Idempotency-Key` | La del flujo (tabla de abajo), cuando el destino la pide |
| `X-Request-Id` | Id del flujo (el del Gateway si llega, si no uno nuevo) para seguirlo en los logs |
| `Content-Type` | `application/json` |

### Claves de idempotencia

| Flujo | Clave hacia el destino |
| --- | --- |
| Registro (billetera) | `wallet:{user_id}` |
| Compra | `purchase:{user_id}:{clave del front}` |
| Inscripción | `event-entry:{evento_id}:{user_id}` (la que ya valida el PR #21) |
| Apuesta | la clave del front, tal cual; el Motor la combina con el `user_id` |

### Timeouts y reintentos (propuesta técnica)

- Timeout de 3 s por llamada.
- Hasta 3 intentos, con esperas de 200 ms y 400 ms entre ellos.
- Solo se reintenta ante error de red, timeout, 502, 503 o 504, y solo en llamadas idempotentes: GET y POST con `Idempotency-Key`. Nunca se reintenta un 4xx.
- Si se agotan los intentos → 503 `servicio_no_disponible` (o un aviso, en la página SSR).

### Traducción de errores

Cada cliente (Usuarios, Pagos, Motor) va detrás de una interfaz y traduce los códigos de su servicio a la tabla de errores de §2. Hoy Pagos responde `{detail}` (FastAPI) y el Motor `{error}`; el Orquestador nunca reenvía esos cuerpos tal cual al front.

## 5. Rutas internas que faltan

Rutas que el Orquestador necesita y que todavía no existen. Todas exigen `X-Internal-Service-Token`.

| Servicio | Ruta | Para | Por acordar con |
| --- | --- | --- | --- |
| Usuarios | `POST /internal/users {id, nombre, email, contrasena}` → 201 · 200 si el `id` ya existe con el mismo correo · 409 correo repetido | Registro | Misael (HU2-01) |
| Usuarios | Usuarios acepta el `id` que manda el Orquestador | Registro | Misael (HU2-01) |
| Usuarios | `GET /internal/users/{id}` → `{id, nombre, email, rol}` | Página SSR | Misael (HU2-03) |
| Pagos | `POST /internal/wallets {user_id}` + `Idempotency-Key` → 201 · 200 ya existía | Registro | Juan Diego (T2-03) |
| Pagos | `GET /internal/wallets/{user_id}` → `{balance, cop_por_token}` | SSR, billetera | Juan Diego (T2-03, HU2-04) |
| Pagos | `POST /internal/purchases {user_id, monto_cop}` + `Idempotency-Key` | Compra | Juan Diego (HU2-04) |
| Pagos | `GET /internal/users/{user_id}/event-entries` | Página SSR | Misael (HU2-03) con Juan Diego |
| Pagos | `GET /internal/users/{user_id}/bets?status=pending` | Página SSR | Misael (HU2-03) con Juan Diego |
| Pagos | `GET /internal/users/{user_id}/ledger?limit=10` | Página SSR | Misael (HU2-03) con Juan Diego |
| Pagos | `GET /internal/events?status=programado,en_curso` | Calendario | Tomás (HU2-05) |
| Pagos | `GET /internal/events/{id}/markets?kind=estatica` | Calendario | Tomás (HU2-05, HU2-07) |
| Pagos | `POST /internal/events/{id}/registrations` y `GET …/registrations/{user_id}` | Inscripción, calendario | Michael (HU2-06, PR #21: existen; falta alinear nombres) |
| Motor | `POST /internal/apuestas {user_id, mercado_id, opcion, monto}` + `Idempotency-Key`; reemplaza `POST /apuestas` con JWT | Apuesta | Michael (HU2-09) |
| Motor | `GET /internal/torneos/{evento_id}/jugadores/{user_id}` → `{fichas, puesto, fichas_apostadas}` | Página SSR | Guillermo (HU2-11) |

> **Nota sobre el PR #21:** este contrato usa los nombres del documento del P2. El PR #21 usa `event_registrations` (doc: `event_entries`), los estados `registration` / `multiplayer` / `individual_only` / `finished` (doc: `status` programado \| en_curso \| finalizado y `tournament_status` abierto \| activo \| cancelado \| repartido), los tipos de ledger `event_entry` / `event_refund` (doc: `buy_in` / `refund`), `race_ref` (doc: `race_key`) y un 403 por rol en Pagos (el rol se valida en el Orquestador). Las rutas `/internal/events/{id}/registrations` se mantienen.

## 6. Preguntas abiertas

Sin preguntas abiertas por ahora.
