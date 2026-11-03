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

# Dentro de Docker Compose, Admin se alcanza por su nombre en la red "borde".
ENV POLEBET_ADMIN_URL=http://admin:8003

# Sin root: la CLI solo necesita la terminal.
USER nobody

ENTRYPOINT ["polebet-cli"]
