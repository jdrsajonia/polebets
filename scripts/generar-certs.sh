#!/usr/bin/env bash
# Escenario 1 (Secure Channel) y T3-06: genera los certificados TLS locales del
# Gateway con la receta del Lab 5 (una CA privada que firma el certificado del
# servidor, llaves EC P-256).
#
# Los certificados no se suben al repo (services/gateway/certs/ está en
# .gitignore), así que cada quien genera los suyos antes del primer
# `docker compose up`. Sin ellos Nginx no arranca.
#
# Deja en services/gateway/certs/:
#   ca.crt        CA local: es lo que los clientes usan para confiar en el Gateway
#                 (curl --cacert, la CLI con --ca, el navegador si se importa)
#   ca.key        llave de la CA (secreta, solo sirve para firmar)
#   polebet.crt   certificado del Gateway, firmado por la CA
#   polebet.key   llave del Gateway (secreta)
#
# El certificado vale para localhost y 127.0.0.1 (navegador, curl y la CLI
# desde el host) y para gateway (la CLI dentro de Docker Compose). Si el nombre
# con el que el cliente llama no está aquí, el cliente lo rechaza.
#
# Uso, desde la raíz del repo (en Windows, con Git Bash):
#   scripts/generar-certs.sh            # no toca certificados que ya existan
#   scripts/generar-certs.sh --forzar   # los reemplaza (nueva CA incluida)
#
# Usa openssl si está instalado; si no, la imagen alpine/openssl de Docker.
#
# Variables opcionales:
#   CERT_DIAS   días de validez de la CA y del certificado (por defecto 365; el
#               Lab 5 usa 30, pero vencerían antes de terminar el semestre)
set -euo pipefail

export MSYS_NO_PATHCONV=1 # Git Bash no debe convertir las rutas de los contenedores

cd "$(dirname "$0")/.."

DIR=services/gateway/certs
DIAS="${CERT_DIAS:-365}"
NOMBRES="DNS:localhost, DNS:gateway, IP:127.0.0.1"

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

# No reemplaza una CA en uso sin pedirlo: los clientes que ya confían en ella
# dejarían de aceptar el Gateway.
if [[ -f "$DIR/polebet.crt" && -f "$DIR/polebet.key" && $forzar -eq 0 ]]; then
    echo "Ya hay certificados en $DIR. Para reemplazarlos: scripts/generar-certs.sh --forzar"
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
        docker run --rm --user "$(id -u):$(id -g)" -v "$PWD/$DIR:/w" -w /w alpine/openssl "$@"
    }
fi

# Los .cnf, la solicitud (CSR) y el serial solo se usan mientras se genera.
trap 'rm -f "$DIR/ca.cnf" "$DIR/server.cnf" "$DIR/polebet.csr" "$DIR/ca.srl"' EXIT

# Datos de la CA (igual que ca.cnf del Lab 5).
cat >"$DIR/ca.cnf" <<'EOF'
[req]
prompt = no
distinguished_name = dn
x509_extensions = ca

[dn]
CN = PoleBet CA local

[ca]
basicConstraints = critical, CA:TRUE, pathlen:0
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
EOF

# Datos del certificado del Gateway (server.cnf del Lab 5 con gateway en el SAN).
cat >"$DIR/server.cnf" <<EOF
[req]
prompt = no
distinguished_name = dn

[dn]
CN = localhost

[server]
basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = $NOMBRES
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid, issuer
EOF

# CA: llave y certificado autofirmado.
ossl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out ca.key
ossl req -x509 -new -key ca.key -sha256 -days "$DIAS" -config ca.cnf -out ca.crt

# Gateway: llave, solicitud de firma y certificado firmado por la CA.
ossl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out polebet.key
ossl req -new -key polebet.key -config server.cnf -out polebet.csr
ossl x509 -req -in polebet.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -sha256 -days "$DIAS" -extfile server.cnf -extensions server -out polebet.crt

# Las llaves solo para el dueño; los certificados son públicos.
chmod 600 "$DIR/ca.key" "$DIR/polebet.key"
chmod 644 "$DIR/ca.crt" "$DIR/polebet.crt"

# Comprueba la cadena y cada nombre del SAN; si alguno falla, set -e corta aquí.
ossl verify -CAfile ca.crt -verify_hostname localhost polebet.crt
ossl verify -CAfile ca.crt -verify_hostname gateway polebet.crt
ossl verify -CAfile ca.crt -verify_ip 127.0.0.1 polebet.crt

cat <<EOF

Listo: certificados en $DIR (válidos por $DIAS días, para $NOMBRES).

Si el Gateway ya estaba arriba, reinícialo para que cargue los nuevos:
  docker compose restart gateway

Para comprobarlo sin apagar la verificación (nunca uses -k):
  curl --cacert $DIR/ca.crt https://localhost/health

En el navegador, importa $DIR/ca.crt como autoridad de confianza,
o acepta el aviso de certificado no reconocido.
EOF
