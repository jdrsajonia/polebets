# ---- build ----
FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencias primero para aprovechar la cache de Docker. El orquestador solo
# usa la libreria estandar, asi que no hay go.sum; si algun dia se agrega una
# dependencia, se copia aqui junto a go.mod (no se corre go mod tidy).
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /orquestador .

# ---- runtime ----
FROM alpine:3.20

# wget (incluido en busybox de alpine) sirve para el healthcheck del compose:
# wget -qO- http://localhost:8081/health
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /orquestador /app/orquestador
USER app

EXPOSE 8081
ENTRYPOINT ["/app/orquestador"]
