#!/usr/bin/env bash
# Escenario 4 (autenticación entre servicios, T3-05): genera los tokens de
# servicio, uno por cada par llamador → destino, y los escribe en el .env.
#
# Antes había un solo INTERNAL_API_TOKEN en cinco contenedores, y con él
# cualquier servicio podía llamar a cualquier otro. Ahora cada par tiene el
# suyo y cada servicio acepta solo los tokens de quienes deben llamarlo:
#   TOKEN_ORQUESTADOR_USUARIOS   Orquestador → Usuarios
#   TOKEN_ORQUESTADOR_PAGOS      Orquestador → Pagos
#   TOKEN_ORQUESTADOR_MOTOR      Orquestador → Motor
#   TOKEN_MOTOR_PAGOS            Motor → Pagos
# Un token solo lo tienen los dos extremos del par: aunque alguien lo saque de
# Pagos, no le sirve contra Usuarios ni contra el Motor.
#
# El .env.example ya trae valores de ejemplo distintos (sirven para la demo
# local); este script los cambia por valores aleatorios de 64 caracteres.
#
# Uso, desde la raíz del repo (en Windows, con Git Bash):
#   scripts/generar-tokens-servicio.sh            # agrega solo los que falten
#   scripts/generar-tokens-servicio.sh --forzar   # los reemplaza todos
#
# Usa openssl si está instalado; si no, python3, y si no, la imagen
# alpine/openssl de Docker.
set -euo pipefail

export MSYS_NO_PATHCONV=1 # Git Bash no debe convertir las rutas de los contenedores

cd "$(dirname "$0")/.."

ENV=.env
TOKENS=(TOKEN_ORQUESTADOR_USUARIOS TOKEN_ORQUESTADOR_PAGOS TOKEN_ORQUESTADOR_MOTOR TOKEN_MOTOR_PAGOS)

# Muestra la ayuda (las líneas de comentario del encabezado).
ayuda() {
    sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
}

# Lee los argumentos: solo --forzar y -h.
forzar=0
for arg in "$@"; do
    case "$arg" in
        --forzar) forzar=1 ;;
        -h | --help)
            ayuda
            exit 0
            ;;
        *)
            echo "Argumento desconocido: $arg (usa --forzar o -h)" >&2
            exit 2
            ;;
    esac
done

if [[ ! -f $ENV ]]; then
    echo "No hay $ENV en la raíz. Créalo primero: cp .env.example .env" >&2
    exit 1
fi

# Imprime un token aleatorio de 32 bytes en hexadecimal (64 caracteres).
nuevo_token() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32
    elif command -v python3 >/dev/null 2>&1; then
        python3 -c 'import secrets; print(secrets.token_hex(32))'
    else
        docker run --rm alpine/openssl rand -hex 32
    fi
}

# Escribe NOMBRE=valor en el .env: reemplaza la línea si existe, si no la
# agrega al final. Se reescribe con un archivo temporal para no depender de
# las diferencias de sed -i entre Linux y macOS.
escribir() {
    local nombre=$1 valor=$2 tmp
    tmp=$(mktemp)
    if grep -q "^$nombre=" "$ENV"; then
        awk -v n="$nombre" -v v="$valor" 'index($0, n "=") == 1 { print n "=" v; next } { print }' "$ENV" >"$tmp"
    else
        cat "$ENV" >"$tmp"
        printf '%s=%s\n' "$nombre" "$valor" >>"$tmp"
    fi
    cat "$tmp" >"$ENV" # conserva los permisos del .env
    rm -f "$tmp"
}

cambiados=()
for nombre in "${TOKENS[@]}"; do
    if [[ $forzar -eq 1 ]] || ! grep -q "^$nombre=." "$ENV"; then
        escribir "$nombre" "$(nuevo_token)"
        cambiados+=("$nombre")
    fi
done

if [[ ${#cambiados[@]} -eq 0 ]]; then
    echo "El $ENV ya tiene los cuatro tokens. Para reemplazarlos: scripts/generar-tokens-servicio.sh --forzar"
else
    echo "Listo: ${cambiados[*]} en $ENV."
    echo
    echo "Si el sistema ya estaba arriba, recrea los servicios que los usan:"
    echo "  docker compose up -d --force-recreate orquestador usuarios pagos motor"
fi

if grep -q "^INTERNAL_API_TOKEN=" "$ENV"; then
    echo
    echo "Nota: INTERNAL_API_TOKEN ya no lo usa ningún servicio; puedes borrarlo del $ENV."
fi
