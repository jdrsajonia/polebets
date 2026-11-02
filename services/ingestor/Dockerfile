# Ingestor (HU2-16): descarga carreras de OpenF1 y las reproduce en MongoDB.
# ---- build ----
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
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
# Por defecto arranca el servicio; la descarga se corre aparte:
#   docker exec arqui_ingestor /app/ingestor descargar <session_key>
ENTRYPOINT ["/app/ingestor"]
CMD ["servicio"]
