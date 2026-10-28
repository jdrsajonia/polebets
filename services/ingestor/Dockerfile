# Ingestor — STUB base (T2-04). Mismo patrón que services/motor y services/orquestador.
# ---- build ----
FROM golang:1.25-alpine AS build

WORKDIR /src

# El stub solo usa la librería estándar, así que no hay go.sum todavía. Cuando
# se agreguen dependencias (cliente de RabbitMQ, driver de MongoDB), se copia
# go.sum aquí junto a go.mod.
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /ingestor .

# ---- runtime ----
FROM alpine:3.20

# wget (incluido en busybox de alpine) se usa en el healthcheck del compose.
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /ingestor /app/ingestor
USER app

EXPOSE 8002
ENTRYPOINT ["/app/ingestor"]
