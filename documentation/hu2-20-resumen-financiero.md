# HU2-20 · Resumen financiero

Como administrador, quiero ver cuánto entra y cuánto sale, para saber si la plataforma gana o pierde (RF-25).

La consulta la responde **Pagos**, por el mismo camino que la rentabilidad (HU2-21): la CLI llama a Admin (`POST /consultas`), Admin la publica en `admin.consultas` y el consumidor de Pagos responde por `reply_to` con el mismo `correlation_id`.

## Qué calcula

Todo sale de `ledger_entries` (RNF-20), sumando `tokens_delta` por tipo de movimiento:

| Dato | Cálculo | Lo escribe |
| --- | --- | --- |
| `tokens_comprados` | suma de `purchase` | HU2-04 |
| `cop_comprados` | `tokens_comprados` × `COP_PER_TOKEN` | — |
| `apostado_individual` | − suma de `debit` (incluye apuestas sin resolver) | HU2-10 |
| `pagado_individual` | suma de `settle` | HU2-10 |
| `inscripciones_cobradas` | − suma de `buy_in` | HU2-06 |
| `inscripciones_devueltas` | suma de `refund` | HU2-06 |
| `premios_pagados` | suma de `prize` | HU2-12 |
| `ganancia_neta` | apostado − pagado + inscripciones − devueltas − premios | — |
| `botin_en_juego` | `events.pot` de los torneos `abierto` o `activo` | — |
| `apostado_pendiente` | monto de las apuestas individuales `pending` | — |

En un evento ya repartido, la parte del torneo de la ganancia es exactamente comisión + residuo, que es como gana la plataforma según las reglas del juego. En un evento que sigue en curso, la ganancia todavía incluye el botín y las apuestas sin resolver; por eso se informa aparte lo que sigue en juego.

Las apuestas de torneo no cuentan en ningún dato: se pagan en fichas, que no pasan por el ledger.

## Filtros (RF-29)

Cada dato pertenece a un modo: las compras a ninguno, las apuestas individuales al individual, y las inscripciones, premios y botín al torneo.

| Filtro (Admin → Pagos) | Efecto |
| --- | --- |
| `evento_id` → `event_id` | A todo. Las compras no tienen evento: con este filtro salen en 0 |
| `fecha_inicio` / `fecha_fin` → `desde` / `hasta` | A todo (fecha del movimiento; en lo pendiente, fecha de la apuesta), salvo `botin_en_juego`, que es una foto del momento |
| `modo` → `modo` | Deja solo los datos de ese modo |
| `tipo_apuesta` → `kind` | Deja solo las apuestas individuales de ese tipo (por el mercado de la apuesta) |
| `plantilla`, `top_n` | No aplican: Admin no los manda y los devuelve en `filtros_no_aplicados` |

Con `modo` o `tipo_apuesta`, las compras salen en 0: no son apuestas.

## Contrato del Broker

```json
// Consulta (Admin → admin.consultas)
{ "tipo": "resumen_financiero",
  "filtros": { "event_id": "…", "desde": "…", "hasta": "…", "kind": "estatica", "modo": "individual" } }

// Respuesta (Pagos → reply_to)
{ "tipo": "resumen_financiero", "ok": true,
  "datos": { "tokens_comprados": 300, "cop_comprados": 300000, "apostado_individual": 0,
             "pagado_individual": 0, "inscripciones_cobradas": 100, "inscripciones_devueltas": 0,
             "premios_pagados": 0, "ganancia_neta": 100, "botin_en_juego": 100, "apostado_pendiente": 0 } }
```

Un filtro desconocido o con un valor inválido responde `{"ok": false, "error": "filtros_invalidos"}`.

## Decisiones

1. **Se cuenta lo registrado y se informa aparte lo que está en juego.** No se esperan a que los eventos terminen: el administrador ve la ganancia hasta ahora y cuánto de ella todavía puede salir.
2. **«Apostado» no es lo mismo que en la rentabilidad.** HU2-21 cuenta solo apuestas resueltas; aquí `apostado_individual` incluye las pendientes, porque son tokens que ya salieron de las billeteras (RF-25: «tokens apostados»). Lo pendiente va también aparte en `apostado_pendiente`.
3. **El equivalente en COP usa la tasa actual.** No hay columna en COP (punto abierto del modelo de datos).
4. **Qué filtros aplica cada consulta lo decide Admin**, igual que ya hacía con `modo` en la rentabilidad. Pagos tiene un modelo de filtros propio para esta consulta (`FiltrosResumen`) y no cambia el de la rentabilidad.

## Archivos

- `services/pagos/app/services/resumen_financiero.py`: la consulta.
- `services/pagos/app/schemas/consultas.py`: `FiltrosResumen`, la consulta y la respuesta.
- `services/pagos/app/services/consulta_handler.py`: elige la consulta según el `tipo`.
- `services/admin/app/main.py`: acepta `resumen_financiero`, manda `modo` y decide qué filtros aplica cada consulta.
- `services/pagos/tests/test_resumen_financiero.py`.

## Probar a mano

Con el stack arriba (Admin publica el puerto 8003):

```bash
curl -X POST localhost:8003/consultas -H 'Content-Type: application/json' \
  -d '{"query":"resumen_financiero","params":{"modo":"torneo"}}'
```
