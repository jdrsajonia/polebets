#!/usr/bin/env bash
# Corre la prueba de login nivel por nivel (un archivo JSON por nivel).
# Antes de cada nivel reinicia 'usuarios' para que todos empiecen desde el mismo estado.
#
# Uso (Git Bash, desde la raíz del repo):
#   export EMAIL="jugador@prueba.com"; export PASSWORD="12345678"
#   PREFIJO=antes bash scripts/correr_niveles.sh        # rama p2/sin-seguridad
#   PREFIJO=despues bash scripts/correr_niveles.sh      # rama con rate limiting

PREFIJO="${PREFIJO:-antes}"
NIVELES="${NIVELES:-1 25 50 75 100 125 150 175 200 500}"
: "${EMAIL:?Define EMAIL}"
: "${PASSWORD:?Define PASSWORD}"

mkdir -p resultados
for v in $NIVELES; do
  echo "=== $PREFIJO: $v usuarios ==="
  docker compose restart usuarios
  for i in $(seq 1 30); do
    docker compose ps usuarios | grep -q "(healthy)" && break
    sleep 2
  done
  sleep 5
  VUS=$v k6 run --log-output=none --summary-export="resultados/$PREFIJO-$v.json" scripts/performance_niveles.js
  sleep 20
done
echo "Listo. Archivos en resultados/$PREFIJO-*.json"
