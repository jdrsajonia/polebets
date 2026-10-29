#!/usr/bin/env bash
# T2-03: pasa los datos del prototipo 1 a las bases del prototipo 2.
#
#   users del P1 (sin balance ni points) -> users de PostgreSQL Usuarios (mismo id)
#   users.balance del P1                 -> wallets de PostgreSQL Pagos
#
# Al final compara cuántos usuarios y cuántos tokens hay a cada lado y falla si
# no coinciden: «cada saldo pasa a wallets sin perder ningún token».
#
# No se migra el historial del P1 (bets, ledger_entries): sus apuestas apuntan a
# mercados de MongoDB, que en el P2 no existen como filas de markets.
#
# Antes de correrlo:
#   1. El stack del P2 arriba (docker compose up), para que Usuarios y Pagos ya
#      hayan creado sus tablas. Deben estar vacías: el script no mezcla datos.
#   2. El volumen de PostgreSQL del P1 en esta máquina. El script lo abre en un
#      contenedor temporal sin red y lo apaga al terminar; no lo modifica.
#
# Uso, desde la raíz del repo (en Windows, con Git Bash):
#   scripts/migrar-p1.sh
#
# Variables opcionales:
#   P1_VOLUME   volumen del P1   (por defecto arquisoft-prototipo1_postgres_data)
#   P1_DB_USER  usuario del P1   (por defecto arqui)
#   P1_DB_NAME  base del P1      (por defecto apuestas_db, el POSTGRES_DB del .env del P1)
set -euo pipefail

export MSYS_NO_PATHCONV=1 # Git Bash no debe convertir las rutas de los contenedores

cd "$(dirname "$0")/.."
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

P1_VOLUME="${P1_VOLUME:-arquisoft-prototipo1_postgres_data}"
P1_DB_USER="${P1_DB_USER:-arqui}"
P1_DB_NAME="${P1_DB_NAME:-apuestas_db}"
P1_CONTAINER="arqui_pg_p1_migracion"
USUARIOS_CONTAINER="${USUARIOS_CONTAINER:-arqui_pg_usuarios}"
PAGOS_CONTAINER="${PAGOS_CONTAINER:-arqui_pg_pagos}"

falla() {
  echo "ERROR: $*" >&2
  exit 1
}

p1() { docker exec -i "$P1_CONTAINER" psql -U "$P1_DB_USER" -d "$P1_DB_NAME" -v ON_ERROR_STOP=1 -qAt "$@"; }
usuarios() { docker exec -i "$USUARIOS_CONTAINER" psql -U "$USUARIOS_DB_USER" -d "$USUARIOS_DB_NAME" -v ON_ERROR_STOP=1 -qAt "$@"; }
pagos() { docker exec -i "$PAGOS_CONTAINER" psql -U "$PAGOS_DB_USER" -d "$PAGOS_DB_NAME" -v ON_ERROR_STOP=1 -qAt "$@"; }

for v in USUARIOS_DB_USER USUARIOS_DB_NAME PAGOS_DB_USER PAGOS_DB_NAME; do
  [[ -n "${!v:-}" ]] || falla "falta $v en .env (ver .env.example)"
done
docker volume inspect "$P1_VOLUME" >/dev/null 2>&1 || falla "no existe el volumen $P1_VOLUME"
for c in "$USUARIOS_CONTAINER" "$PAGOS_CONTAINER"; do
  [[ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" == "true" ]] || falla "$c no está corriendo (docker compose up)"
done

[[ "$(usuarios -c 'SELECT count(*) FROM users')" == "0" ]] || falla "users de PostgreSQL Usuarios no está vacía"
[[ "$(pagos -c 'SELECT count(*) FROM wallets')" == "0" ]] || falla "wallets de PostgreSQL Pagos no está vacía"

echo "Abriendo el volumen del P1 ($P1_VOLUME)..."
docker run -d --rm --name "$P1_CONTAINER" --network none \
  -v "$P1_VOLUME":/var/lib/postgresql/data postgres:17-alpine >/dev/null
trap 'docker stop "$P1_CONTAINER" >/dev/null 2>&1 || true' EXIT
for _ in $(seq 1 30); do
  docker exec "$P1_CONTAINER" pg_isready -q -U "$P1_DB_USER" 2>/dev/null && break
  sleep 1
done
p1 -c 'SELECT 1' >/dev/null || falla "no se pudo abrir la base $P1_DB_NAME del P1 (revisa P1_DB_USER y P1_DB_NAME)"

echo "Copiando users..."
p1 -c "\copy (SELECT id, email, name, password_hash, role, created_at FROM users) TO STDOUT" |
  usuarios -c "\copy users (id, email, name, password_hash, role, created_at) FROM STDIN"

echo "Copiando saldos a wallets..."
p1 -c "\copy (SELECT id, balance FROM users) TO STDOUT" |
  pagos -c "\copy wallets (user_id, balance) FROM STDIN"

origen="$(p1 -c 'SELECT count(*) || '"' usuarios, '"' || coalesce(sum(balance), 0) || '"' tokens'"' FROM users')"
en_usuarios="$(usuarios -c 'SELECT count(*) FROM users')"
en_pagos="$(pagos -c 'SELECT count(*) || '"' usuarios, '"' || coalesce(sum(balance), 0) || '"' tokens'"' FROM wallets')"

echo "P1:                 $origen"
echo "PostgreSQL Usuarios: $en_usuarios usuarios"
echo "PostgreSQL Pagos:    $en_pagos"
[[ "$origen" == "$en_pagos" && "${origen%% *}" == "$en_usuarios" ]] || falla "los totales no coinciden"
echo "Migración completa: no se perdió ningún token."
