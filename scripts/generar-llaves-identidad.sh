#!/usr/bin/env bash
# Escenario 4 (autenticación entre servicios, T3-05): genera el par de llaves
# Ed25519 con el que Usuarios firma la identidad de cada petición.
#
# Usuarios firma con la llave PRIVADA un token corto (sub, rol, 60 s) en
# GET /internal/auth; el Gateway lo reenvía en X-User-Token y el Orquestador y
# el Motor lo verifican con la llave PÚBLICA. Con la pública solo se verifica:
# aunque alguien la saque de un contenedor, no puede fabricar identidades.
#
# Las llaves no se suben al repo (services/usuarios/llaves/ está en
# .gitignore), así que cada quien genera las suyas antes del primer
# `docker compose up`. Sin ellas Usuarios, el Orquestador y el Motor no arrancan.
#
# Deja en services/usuarios/llaves/:
#   identidad.key   llave privada (secreta): solo la monta Usuarios
#   identidad.pub   llave pública: la montan el Orquestador y el Motor
#
# Uso, desde la raíz del repo (en Windows, con Git Bash):
#   scripts/generar-llaves-identidad.sh            # no toca llaves que ya existan
#   scripts/generar-llaves-identidad.sh --forzar   # las reemplaza
#
# Usa openssl si está instalado; si no, la imagen alpine/openssl de Docker.
set -euo pipefail

export MSYS_NO_PATHCONV=1 # Git Bash no debe convertir las rutas de los contenedores

cd "$(dirname "$0")/.."

DIR=services/usuarios/llaves

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

# No reemplaza un par en uso sin pedirlo: los tokens firmados con la llave
# anterior dejarían de verificar (duran 60 s, pero hay que reiniciar los tres
# servicios para que carguen el par nuevo).
if [[ $forzar -eq 0 && -f "$DIR/identidad.key" && -f "$DIR/identidad.pub" ]]; then
    echo "Ya hay llaves en $DIR. Para reemplazarlas: scripts/generar-llaves-identidad.sh --forzar"
    exit 0
fi

mkdir -p "$DIR"

# Corre openssl dentro de $DIR: el local si existe, si no el de Docker (con el
# usuario actual, para que los archivos no queden a nombre de root).
if command -v openssl >/dev/null 2>&1; then
    ossl() { (cd "$DIR" && openssl "$@"); }
else
    echo "openssl no está instalado; se usa la imagen alpine/openssl de Docker."
    ossl() {
        # :z reetiqueta la carpeta para SELinux (Fedora); Docker Desktop lo ignora.
        docker run --rm --user "$(id -u):$(id -g)" -v "$PWD/$DIR:/w:z" -w /w alpine/openssl "$@"
    }
fi

# Par Ed25519: la privada en PKCS#8 y la pública en SubjectPublicKeyInfo (PEM),
# los formatos que leen PyJWT (cryptography) y golang-jwt.
ossl genpkey -algorithm ed25519 -out identidad.key
ossl pkey -in identidad.key -pubout -out identidad.pub

# La privada solo para el dueño (Usuarios corre como root y la lee igual); la
# pública la leen el Orquestador y el Motor, que corren como el usuario 10001.
chmod 600 "$DIR/identidad.key"
chmod 644 "$DIR/identidad.pub"

cat <<EOF

Listo: llaves de identidad en $DIR.

Si el sistema ya estaba arriba, reinicia los servicios que las usan:
  docker compose up -d --force-recreate usuarios orquestador motor
EOF
