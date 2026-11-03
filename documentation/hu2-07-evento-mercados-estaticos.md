# HU2-07 — Evento programado y apuestas estáticas

Dueño: **Tomás**. Componente: **Motor (Gestor de mercados estáticos)** + Pagos.

## Qué hace

El Gestor de estáticas es un worker del Motor que vigila las simulaciones en
MongoDB (que escribe el Ingestor, solo lectura — RNF-18) y orquesta el ciclo
de vida del evento y sus mercados estáticos contra Pagos (REST interno):

1. **Simulación `programada`** → crea el evento en Pagos (`POST /internal/events`)
   y abre el/los mercado(s) estático(s) del catálogo (`POST /internal/markets`
   con `status: abierto`), cada opción con su multiplicador.
2. **Largada (simulación `en_curso`)** → cierra los mercados estáticos
   (`POST /internal/markets/{id}/close`) y las inscripciones
   (`POST /internal/events/{id}/start`); con los jugadores que devuelve `start`,
   inicializa las fichas del torneo (HU2-11, `fichas.InicializarTorneo`).
3. **Fin de carrera (simulación `finalizada`)** → resuelve cada estática
   (`POST /internal/markets/{id}/resolve` con la opción ganadora leída de los
   datos reales de la carrera). El worker de HU2-10 (`liquidar/`) paga las
   apuestas individuales; este Gestor no paga.

Todo se decide con el **tiempo de carrera** / el estado de la simulación, no
con la hora real (RNF-19). El ciclo es idempotente (RNF-12/20): reprocesar la
misma simulación no duplica eventos ni mercados, porque Pagos es idempotente
por `event_id` / `market_id` y el Motor los deriva determinísticamente.

## Contrato de ids (acordado con HU2-08, Daniel)

- `event_id` en Pagos = **`_id` de la simulación** en Mongo (ya es UUID). Así el
  Gestor de dinámicas (HU2-08) cuelga sus mercados del mismo evento sin que nos
  pasemos el id. Se usa en formato UUID canónico (minúsculas, con guiones).
- `race_key` del evento = **`carrera_id`** de la simulación (p. ej. `2024_monza`).
- `market_id` de una estática = `uuidV5(espacio, "{event_id}|estatica|{template}")`,
  determinista para que un reinicio no duplique mercados.

## Catálogo estático (prototipo: una plantilla sólida)

Se empieza con **"ganador"** — "¿Quién gana la carrera?":

- **Opciones** = los pilotos de la carrera (`carreras.pilotos[].codigo`, el
  código de 3 letras: VER, NOR, …).
- **Multiplicador** por tramos, según el nivel del piloto (clasificación F1
  2024). Lo fija el Motor (no sale de OpenF1):

  | Tramo | Pilotos (código) | Multiplicador |
  | --- | --- | --- |
  | Top | VER, NOR, LEC, PIA | 3.0 |
  | Alto | SAI, RUS, HAM | 6.0 |
  | Medio | PER, ALO, GAS, HUL | 10.0 |
  | Resto / desconocido | cualquier otro | 15.0 |

- **Resolución**: el ganador es el piloto cuyo último evento `posicion` (mayor
  `t_carrera_ms`) tiene `position == 1`, en `eventos_carrera`. Ese
  `driver_number` se mapea a su `codigo` para casar con la opción.

El catálogo es una tabla en el Motor; añadir plantillas (podio, vuelta rápida)
es sumar entradas, sin tocar el worker.

## Estructura (paquete `services/motor/internal/estaticas/`)

Sigue el patrón de `dinamicas/` (HU2-08):

| Archivo | Qué hace |
| --- | --- |
| `uuid.go` | `uuidV5` para `event_id`/`market_id` deterministas |
| `catalogo.go` | multiplicadores por tramo + arma opciones "ganador" (puro) |
| `resolver.go` | ganador = último `position==1` (puro) |
| `gestor.go` | máquina de estados: estado de simulación → acciones (puro) |
| `pagos.go` | cliente REST: crear evento + crear/cerrar/resolver mercado |
| `mongo.go` | lee simulación, pilotos y eventos `posicion` (solo lectura) |
| `worker.go` | bucle `Run(ctx)`: aplica las acciones; llama a fichas al largar |

## Fronteras con otras HU

- **HU2-06 (Michael, Pagos):** `POST /internal/events/{id}/start` ya existe; lo
  invoca este Gestor en la largada (nadie más lo llamaba).
- **HU2-11 (Guillermo, Motor):** `fichas.InicializarTorneo` ya existe; este
  Gestor lo llama con los jugadores de `start`. Es idempotente.
- **HU2-10 (Motor):** paga las apuestas individuales de los mercados resueltos.
  Este Gestor solo escribe `winning_option`, no paga.
- **HU2-08 (Daniel, Motor):** comparte el `event_id`; no crea el evento (lo crea
  este Gestor).
