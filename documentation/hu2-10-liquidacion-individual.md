# HU2-10 · Liquidación individual

**Responsable:** Michael Caicedo · **Servicios:** Motor (worker) y Pagos (dinero)

## Qué hace
Cuando un mercado del P2 ya tiene resultado, el Motor paga en tokens las apuestas
**individuales**: si la opción del jugador es la ganadora recibe `monto × multiplicador`
(truncado); si no, nada (ya se le había debitado al apostar, HU2-09). Al jugador le llega
`resultado_apuesta` por WebSocket. Las apuestas de **torneo** (fichas) no se tocan aquí (HU2-11).

## Flujo
```
Pagos: markets.status = cerrado  +  markets.winning_option = "<opción>"
            │   (lo escribe quien conoce el resultado: HU2-07/08 con el Ingestor)
            ▼
Motor, worker cada 5 s (internal/liquidar)
  1. GET  /internal/markets?status=cerrado&resuelto=true
  2. GET  /internal/markets/{id}/bets?mode=individual&status=pending
  3. POST /internal/settlements  {bet_id, result: won|lost}   Idempotency-Key: bet-settlement:{bet_id}
  4. WebSocket "resultado_apuesta" al dueño de la apuesta
  5. POST /internal/markets/{id}/liquidated   (cerrado → liquidado, solo sin pendientes)
```

## Contrato con quien resuelve los mercados
Para que un mercado se liquide basta con dejarlo en `status = 'cerrado'` y escribir
`winning_option` con el texto exacto de una de sus opciones. HU2-10 hace el resto.

## Garantías
- **No paga dos veces (RNF-12):** la clave de cada pago es `bet-settlement:{bet_id}`;
  una apuesta pagada deja de estar pendiente.
- **Tolerante a fallos:** si un pago falla, el mercado no se marca liquidado y se
  reintenta en la siguiente vuelta; lo ya pagado no se repite.
- El aviso por WebSocket no bloquea: si el jugador no está conectado, el pago igual queda.

## Probar a mano
Con un mercado `cerrado` que tenga apuestas individuales:
```sql
UPDATE markets SET status = 'cerrado', winning_option = 'VER' WHERE id = '<market_id>';
```
En ≤ 5 s las apuestas pasan a `won`/`lost`, el ganador recibe sus tokens y el mercado queda `liquidado`.

## Pendiente
- Que HU2-07/08 + Ingestor escriban `winning_option` en vivo (hoy se pone a mano).
- El worker del P1 (`internal/liquidacion`) sigue liquidando los mercados de Mongo.
