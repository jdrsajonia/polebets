# HU2-08 — Apuestas dinámicas

## Resumen

Durante la carrera el Gestor de mercados del Motor abre mercados de «Sí / No»
cuando se cumple la condición de una plantilla. Solo hay uno abierto a la vez,
con ventana de apuestas de 30 s y observación de 60 s, medidas en **tiempo de
carrera**. Los mercados se guardan en Pagos (`kind = dinamica`,
`template` = id de la plantilla); el liquidador de HU2-10 paga los que quedan
cerrados con `winning_option`.

```
Ingestor ──escribe──▶ MongoDB: eventos_carrera (t_carrera_ms) + simulaciones.t_carrera_ms (reloj)
                          │ (solo lectura)
Motor: Gestor de mercados (internal/dinamicas)
  eventos (cursor, reloj] ─▶ Estado de carrera ─▶ 6 plantillas
  └─ acciones: Crear(abierto) · Cerrar · Resolver(Sí|No) ─▶ Pagos /internal/markets
                                                            └─▶ HU2-10 liquida
```

Estado de la implementación: dominio puro, lector de Mongo, cliente de Pagos,
worker y mensaje WebSocket `mercado_dinamico` (bloques 1 a 3). Falta la
prueba punta a punta con Docker Compose (bloque 4).

## Qué cubre

| ID | Requisito |
| --- | --- |
| RF-11 / RF-13 | Mercados dinámicos que se abren solos según la carrera y se resuelven con lo que pasó |
| RF-24 | Cambios de estado de cada mercado (difusión: bloque 3) |
| RNF-18 | El Motor solo lee MongoDB; los mercados viven en Pagos. Nada en Redis |
| RNF-19 | Abrir, cerrar y resolver se decide solo con el tiempo de carrera |
| RNF-20 | Misma carrera y mismo punto de inicio ⇒ mismos mercados, con los mismos `market_id` |

## Reloj y orden de los eventos

- El «ahora» es `simulaciones.t_carrera_ms`, que el Ingestor escribe
  **después** de marcar los eventos de ese tramo. Por eso, cuando el reloj
  vale R, ya están en Mongo todos los eventos con `t_carrera_ms ≤ R`.
- Se procesan los eventos con `cursor < t_carrera_ms ≤ reloj`, ordenados por
  `(t_carrera_ms, _id)`. Las plantillas se evalúan **evento por evento con el
  t de cada evento**, no por tick del worker.
- Además de los eventos de Mongo, en la misma línea de tiempo hay tres
  momentos internos: el fin de una vuelta (`t + lap_duration`), el cierre
  (`race_close`) y la resolución (`race_close + O`). Regla de orden:
  - una vuelta que termina en x cuenta **antes** de los eventos con t ≥ x;
  - un cierre o una resolución en x ocurre **después** de los eventos con
    t = x. Es decir, «el estado en x» incluye todo lo que pasó en x.
- Con esa regla, procesar la carrera en un solo lote, evento por evento o en
  lotes arbitrarios da exactamente las mismas acciones (hay un test para eso).
- Mientras el reloj no llegue a `race_close + O`, el mercado no se resuelve.

## Estado de carrera

Solo usa lo que se sabe en el instante del evento.

| Tipo | Campos | Uso |
| --- | --- | --- |
| `posicion` | driver_number, position | Orden actual, desempate por número de piloto |
| `intervalo` | driver_number, interval, gap_to_leader | Segundos al de adelante. `null` o texto («+1 LAP») = sin dato |
| `vuelta` | driver_number, lap_number, lap_duration | El evento está fechado al **inicio** de la vuelta; la vuelta «existe» al terminar, en t + lap_duration. Vuelta actual = la mayor empezada (la del líder). Mejor vuelta = mínimo de las ya terminadas |
| `stint` | driver_number, lap_start, tyre_age_at_start, compound | Edad de llantas = vuelta actual del piloto − lap_start + tyre_age_at_start, con el stint más reciente que el piloto ya empezó |
| `pit` | driver_number, t | Entradas a pits en la observación |
| `control` | category, flag, message | Banderas amarillas y despliegue del SC/VSC |

`clima` y `ubicacion` se ignoran. `total_vueltas` y los códigos de 3 letras
salen de `carreras` (si falta el código se usa el número).

### Por qué no se usa `lap_end` ni `pit_duration`

OpenF1 entrega la carrera ya terminada, así que algunas filas traen datos que
en el instante del evento todavía no se conocían:

- `stints.lap_end` dice en qué vuelta se cambiarán las llantas. Usarlo
  revelaría el futuro (por ejemplo, saber que alguien va a entrar a pits) y
  el mercado `entrada_pits` dejaría de ser una apuesta.
- `pit.pit_duration` solo se conoce cuando el auto sale de pits.
- `lap_duration` se conoce al terminar la vuelta, no al empezarla: por eso se
  programa como un evento futuro en t + lap_duration.

El Ingestor fecha los stints con la vuelta del líder, que puede ir adelante
del piloto; un stint cuyo `lap_start` el piloto aún no alcanza se ignora.

## Catálogo

Ventana W = `BETTING_WINDOW_SECONDS` (30 s) y observación O =
`OBSERVATION_WINDOW_SECONDS` (60 s). Mercado abierto en t0:
`race_close = t0 + W`, se resuelve en `race_close + O`. **Toda** resolución
mira solo lo que pasa en `(race_close, race_close + O]` y el estado en
`race_close + O`. Opciones siempre `["Sí", "No"]`.

| # | `template` | Se abre cuando | Pregunta | Sí / No | Gana «Sí» si |
| --- | --- | --- | --- | --- | --- |
| 1 | `adelantamiento` | B a < 1,0 s de A, que va justo delante, con A distinto del líder (varios: menor intervalo; empate por posición) | ¿{B} irá delante de {A} al terminar el próximo minuto? | 3.0 / 1.3 | En race_close+O, B va delante de A |
| 2 | `cambio_lider` | P2 a < 1,5 s del líder L | ¿Habrá un líder distinto de {L} al terminar el próximo minuto? | 3.5 / 1.25 | En race_close+O el P1 no es L |
| 3 | `brecha_se_cierra` | B entre 1,0 y 3,0 s (inclusive) de A, justo delante | ¿{B} estará a menos de 1 s de {A} al terminar el próximo minuto? | 2.5 / 1.5 | En race_close+O, B va delante de A, o A sigue justo delante de B (adyacentes) y el último intervalo de B es < 1,0 s. Con otro auto en medio, «No» |
| 4 | `entrada_pits` | Alguien del top 10 lleva ≥ 18 vueltas con las mismas llantas | ¿Algún piloto del top 10 entrará a pits en el próximo minuto? | 2.2 / 1.6 | Hay un pit en la observación de alguien que estaba en el top 10 en race_close |
| 5 | `vuelta_rapida` | El líder empieza la vuelta 10, 20, 30…, con mejor vuelta conocida M | ¿Habrá una nueva vuelta rápida de la carrera en el próximo minuto? | 2.8 / 1.35 | Una vuelta que **termina** en la observación es < M del **cierre** (si hubo una vuelta rápida durante la ventana, la M de la apertura quedó vieja) |
| 6 | `safety_car` | Llega un control con bandera YELLOW o DOUBLE YELLOW | ¿Saldrá el Safety Car o el VSC en el próximo minuto? | 4.0 / 1.2 | En la observación llega un control con «SAFETY CAR DEPLOYED» (incluye el VSC) |

`adelantamiento` no propone la pareja P1-P2: esa pelea ya la pregunta
`cambio_lider`, y si no, cuando `cambio_lider` está en cooldown saldría la
misma pregunta con otras palabras.

Los ids de `template` no se cambian: HU2-21 los usa en las consultas de
rentabilidad.

## Reglas del Gestor

- **Prioridad** si varias se cumplen en el mismo t: safety_car > cambio_lider
  > adelantamiento > entrada_pits > brecha_se_cierra > vuelta_rapida. Una
  plantilla en cooldown cuenta como «no se cumple» y se prueba la siguiente.
- **Una abierta a la vez.** La siguiente puede abrirse 10 s de carrera después
  de que la anterior **cierra** (no hay que esperar a que se resuelva), así que
  puede haber varias cerradas esperando su resolución.
- **Cooldown:** la misma plantilla, 180 s desde su última apertura; el mismo
  par de pilotos, 300 s, en cualquier plantilla con par (adelantamiento y
  brecha_se_cierra; el par no tiene orden).
- **Vueltas:** no se abre nada antes de la vuelta 2 del líder ni en las 2
  últimas vueltas (`vuelta actual > total_vueltas − 2`). Sin `total_vueltas`
  no se abre nada.
- **Punto de inicio:** no se abre nada con t < `t_inicio_ms`. El primer tick
  del Ingestor marca de golpe todo lo anterior al punto de inicio; con eso solo
  se arma el estado (si no, saldrían mercados «del pasado» que nadie vio).
- **market_id** = UUIDv5(namespace fijo del paquete,
  `simulacion_id + "|" + template + "|" + t0`). Es determinista y es la clave
  de idempotencia de `POST /internal/markets`.
- **event_id** = `_id` de la simulación (acordado con HU2-07).
- **Salida:** acciones ordenadas `Crear(mercado, abierto)`, `Cerrar(id)`,
  `Resolver(id, opción)`, cada una con el t de carrera en que ocurre.
- **Fin de la simulación:** si termina antes de race_close+O (finalizada con
  `t_carrera_ms` menor, o fallida), el mercado abierto se cierra y los cerrados
  quedan sin resolver (el worker los loguea). Si termina después de la
  observación, se resuelve normal.

## Reproducibilidad (RNF-20) y exactitud temporal (RNF-19)

- El dominio no usa `time.Now`, `rand` ni recorre mapas sin ordenar: el orden
  de clasificación se calcula ordenando, y los mapas de cooldown solo se
  consultan por clave.
- Todas las decisiones (abrir, cerrar, resolver) usan `t_carrera_ms`. La hora
  real solo aparecerá en el worker para `opens_at` / `closes_at` de Pagos.
- Los tests `TestDeterminismo` corren una carrera sintética de 30 vueltas
  (sale al menos un mercado de cada plantilla) tres veces —todo junto, evento
  por evento y en lotes de tamaño variable con el reloj entre eventos— y
  comparan las acciones completas, incluidos los `market_id`.

## Adaptadores

### Lector de Mongo (solo lectura, RNF-18)

- **Simulación actual:** la más reciente por `iniciada_en` con estado
  `en_curso`, `finalizada` o `fallida` (las dos últimas sirven para saber que
  terminó). Si aún no hay latido, `t_carrera_ms` queda en `MinInt64`.
- **Carrera:** `total_vueltas` y `pilotos.{numero, codigo}` de `carreras`.
- **Eventos:** `carrera_id`, `tipo` en los que usa el estado y
  `cursor < t_carrera_ms ≤ reloj`, ordenados por `(t_carrera_ms, _id)`. La
  proyección trae solo los campos de `datos.raw` que usa `Normalizar` (nunca
  `lap_end` ni `pit_duration`). El `_id` (ObjectID) se pasa a hex, que
  conserva el orden.

### Cliente de Pagos

| Respuesta | Significado | Qué hace el worker (bloque 3) |
| --- | --- | --- |
| 201 / 200 | Éxito (200 = reintento con los mismos datos o transición ya hecha) | Sigue |
| 404 al crear | El evento aún no existe (HU2-07 no lo creó) | Reintenta en el próximo tick |
| 5xx, 429 o red | Pagos no disponible | Reintenta en el próximo tick |
| 409 | Pagos tiene otros datos para ese mercado | Log fuerte, no reintenta en bucle |
| otro 4xx | Token, validación o mercado inexistente al abrir/cerrar/resolver | Log fuerte, no reintenta |

**`opens_at` y `closes_at` deterministas.** Pagos compara `opens_at` y
`closes_at` cuando llega otra vez el mismo `market_id`: si cambian, responde
409. Por eso no se usa la hora real del momento, sino
`iniciada_en + (t − t_inicio_ms)` (`Simulacion.HoraReal`), que es la misma
fórmula del reloj del Ingestor: en vivo coincide con la hora real y al
reprocesar da exactamente lo mismo. Van en UTC (con zona, como exige Pagos).
`race_close_s` es `race_close` en segundos, redondeado hacia abajo.

## Worker

| Situación | Qué hace |
| --- | --- |
| Simulación `en_curso` sin `t_carrera_ms` | Espera |
| No se pueden leer los datos de la carrera | Reintenta en el próximo tick sin crear el Gestor (se leen una vez por simulación) |
| Acción reintentable que falla (`en_curso`) | Queda en la cola en memoria; no se leen eventos nuevos hasta aplicarla, así nada se duplica |
| 409 u otro rechazo (`en_curso`) | Log `ATENCION` y se salta esa acción |
| Simulación ya terminada al verla | Se procesa una vez; si una acción falla, se abandona hasta que aparezca otra |
| Motor que arranca a mitad de carrera | Reprocesa desde el inicio; Pagos responde 200 a lo ya creado. Si `race_close` ya pasó, crea `programado` y luego cierra |
| Aparece otra simulación | `Terminar()` en el Gestor viejo y aplica sus cierres (un intento) antes de cambiar |

El payload de `mercado_dinamico` lleva `market_id`, `event_id`, `template`,
`question`, `options`, `status` (`abierto` o `cerrado`), `race_close_s`,
`closes_at` y `winning_option` (solo al resolver). `closes_at` es el mismo
`Simulacion.HoraReal(race_close)` que se manda a Pagos, en UTC, para que el
front muestre la cuenta regresiva.

Se difunde `mercado_dinamico` solo en vivo. En el primer lote de una
simulación, una acción más de 5 s detrás del reloj es recuperación y no se
anuncia, ni nada de una simulación que ya estaba terminada.

Mientras el mercado anunciado siga abierto (simulación `en_curso`, su `Crear`
ya aplicado en Pagos y el reloj de carrera antes de `race_close`), el worker
lo reenvía igual cada 5 s de hora real, para quien entra o recarga la página.
La hora real solo decide cuándo reenviar; deja de hacerlo al aplicar el
`Cerrar`.

## Código

`services/motor/internal/dinamicas`:

| Archivo | Qué hace |
| --- | --- |
| `eventos.go` | Tipos normalizados y `Normalizar(tipo, t, id, raw)` (sin lap_end ni pit_duration) |
| `estado.go` | Estado de carrera: orden, intervalos, vueltas, mejor vuelta, edad de llantas |
| `plantillas.go` | Las 6 plantillas: `Evaluar(estado, evento)` y `Resolver(mercado, estado, obs)` |
| `gestor.go` | Máquina de estados: `Avanzar(eventos, reloj) → []Accion` y `Terminar()` |
| `uuid.go` | UUIDv5 con la biblioteca estándar |
| `mongo.go` | Lector de Mongo, solo lectura: simulación actual, datos de la carrera y eventos `(cursor, reloj]` |
| `pagos.go` | Cliente de Pagos: crear, open, close y resolve con el token de servicio |
| `worker.go` | Worker cada 500 ms: cola de acciones, recuperación, cambio de simulación y difusión |

Pruebas: `go test -race ./internal/dinamicas/` desde `services/motor`.
