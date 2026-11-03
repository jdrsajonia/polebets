# HU2-14 · Posiciones en vivo

**Responsable:** Michael Caicedo · **Servicio:** Motor (lee Mongo, difunde por WebSocket)

## Qué hace
Mientras el Ingestor reproduce una carrera (HU2-16/HU2-19), el Motor lee de MongoDB los
eventos de posición que ya "salieron" según el reloj de carrera, actualiza el tablero de HU-05
y difunde la foto por WebSocket con el mensaje `posiciones` (el Front ya lo escucha). La foto
lleva también `vuelta` y `total_vueltas`: el Front muestra «Vuelta X/Y» (RF-19).
`GET /posiciones` devuelve la última foto.

## Contrato con el Ingestor (el único que escribe en Mongo, RNF-18)
| Colección | Campos que lee el Motor |
|---|---|
| `simulaciones` | `_id`, `carrera_id`, `estado` (`en_curso` / `finalizada` / `fallida`), `t_carrera_ms`, `iniciada_en`, `vuelta_actual`, `total_vueltas` |
| `carreras` | `carrera_id`, `pilotos[{numero, codigo, nombre, equipo, color}]` |
| `eventos_carrera` | `carrera_id`, `tipo: "posicion"`, `t_carrera_ms`, `datos.raw.driver_number`, `datos.raw.position` |

Regla clave: el Ingestor actualiza `simulaciones.t_carrera_ms` **después** de marcar los
eventos de ese tramo. El Motor lee los eventos con `t_carrera_ms` en
`(último leído, simulaciones.t_carrera_ms]`, así nunca toma un tramo a medias.

## Flujo (cada 500 ms, `services/motor/internal/carrera`)
1. Simulación más reciente `en_curso`, `finalizada` o `fallida` (por `iniciada_en`); con `fallida` el tablero sigue a la última aunque haya fallado y no se queda en una anterior.
2. Si es nueva: lee los pilotos de `carreras` y reinicia el tablero. La primera lectura incluye
   la parrilla (eventos con `t_carrera_ms` negativo, antes de la largada).
3. Lee los cambios de posición del tramo, se queda con el último de cada carro y difunde **una**
   foto por paso, con la vuelta actual. Si solo cambió la vuelta, también se difunde.
4. Si el Motor se reinicia a mitad de carrera, la primera lectura trae todo hasta el instante
   actual: el tablero queda al día.

## Pendiente
- Diferencias de tiempo (`intervalo`): los datos ya están en Mongo; el tablero hoy muestra
  "Líder" / "—".
