# Ingestor (stub base — T2-04)

Esqueleto del **Ingestor** (Go). Dueño: **Iván** (HU2-16, HU2-19).

Hoy solo responde `GET /health`. La lógica real se construye encima.

## Qué implementar aquí

- Separar `internal/ingest` del Motor a este servicio propio (HU2-16).
- Descargar carreras de **OpenF1** por tramos: posiciones, coordenadas x/y, vueltas,
  pits y control de carrera. La descarga se hace **una sola vez por carrera**, con un comando.
- Recibir por el **Broker (RabbitMQ)** la orden de iniciar simulación (HU2-19) y, a la hora
  de largada, reproducir la carrera según el **tiempo de carrera**, escribiendo cada dato en MongoDB.

## Propiedad de los datos (RNF-18)

Es el **único** componente que **escribe** en MongoDB. El Motor solo lee. Por eso en el
`docker-compose.yml` solo el Ingestor (y Mongo) comparten la red de esa base para escritura.

## Puerto

Interno **8002**. No se publica al host.
