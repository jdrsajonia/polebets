# CLI del administrador (HU2-17), Rust. No es un servicio del stack: es la
# terminal del administrador. La imagen existe para correrla sin instalar Rust:
#
#   docker compose run --rm cli
#
# ---- build ----
FROM rust:1-slim-bookworm AS build

WORKDIR /app

# Primero solo las dependencias, para que Docker las guarde en caché y no las
# recompile cada vez que cambia el código.
COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo 'fn main() {}' > src/main.rs && touch src/lib.rs \
    && cargo build --release --locked && rm -rf src

COPY src ./src
RUN touch src/main.rs src/lib.rs && cargo build --release --locked

# ---- runtime ----
FROM debian:bookworm-slim

COPY --from=build /app/target/release/polebet-cli /usr/local/bin/polebet-cli

# Dentro de Docker Compose la CLI es un cliente externo más: entra por HTTPS al
# Gateway (red "borde"), que la lleva a Admin en /api/admin (escenario 2, T3-03).
# No hace falta ca-certificates ni OpenSSL: rustls trae las CA públicas
# compiladas, y la CA local del Gateway (ca.crt, solo la pública) se monta en
# /certs/ca.crt y se indica con POLEBET_CA_CERT (ver docker-compose.yml, T3-06).
ENV POLEBET_ADMIN_URL=https://gateway/api/admin \
    POLEBET_CA_CERT=/certs/ca.crt

# Sin root: la CLI solo necesita la terminal.
USER nobody

ENTRYPOINT ["polebet-cli"]
