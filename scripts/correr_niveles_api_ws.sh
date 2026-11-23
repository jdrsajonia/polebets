#!/usr/bin/env bash
# Corre la prueba de API (/api/eventos) y de WebSocket (/api/ws) nivel por nivel.
# Antes de cada nivel reinicia el servicio correspondiente para que todos
# empiecen desde el mismo estado (orquestador para la API, motor para WS).
#
# Uso (Git Bash, desde la raíz del repo, en p2/sin-seguridad):
#   export EMAIL="jugador@prueba.com"; export PASSWORD="12345678"
#   bash scripts/correr_niveles_api_ws.sh

NIVELES="${NIVELES:-1 25 50 75 100 125 150 175 200 500}"
: "${EMAIL:?Define EMAIL}"
: "${PASSWORD:?Define PASSWORD}"

mkdir -p resultados

echo "######## API (/api/eventos) ########"
for v in $NIVELES; do
  echo "=== api-despues: $v usuarios ==="
  docker compose restart orquestador
  for i in $(seq 1 20); do
    docker compose ps orquestador | grep -q "(healthy)\|Up" && break
    sleep 2
  done
  sleep 3
  VUS=$v k6 run --log-output=none --summary-export="resultados/api-despues-$v.json" scripts/api_niveles.js
  sleep 5
done

echo "######## WebSocket (/api/ws) ########"
for v in $NIVELES; do
  echo "=== ws-despues: $v usuarios ==="
  docker compose restart motor
  for i in $(seq 1 20); do
    docker compose ps motor | grep -q "(healthy)\|Up" && break
    sleep 2
  done
  sleep 3
  VUS=$v k6 run --log-output=none --summary-export="resultados/ws-despues-$v.json" scripts/ws_niveles.js
  sleep 5
done

echo "Listo. Archivos en resultados/api-despues-*.json y resultados/ws-despues-*.json"
