#!/usr/bin/env bash
# Genera el certificado autofirmado del Gateway (escenarios 1 y 2, P3).
#
#   scripts/generar-certs.sh           # no pisa uno que ya exista
#   scripts/generar-certs.sh --force   # lo regenera
#
# Deja services/gateway/certs/polebet.key y polebet.crt (en .gitignore).
# SAN: localhost (navegador y CLI desde el host), gateway (la CLI dentro de
# Docker llama a https://gateway) y 127.0.0.1.
# CA:FALSE + serverAuth: es un certificado de servidor, no una CA. Por defecto
# `openssl req -x509` pone CA:TRUE y rustls (la CLI) lo rechaza con
# CaUsedAsEndEntity; curl y los navegadores no se quejan, pero la CLI sí.
# Funciona en Linux, macOS y Git Bash de Windows. Después: docker compose restart gateway
set -euo pipefail

# Git Bash convierte "/CN=..." en una ruta de Windows; esto lo evita.
export MSYS_NO_PATHCONV=1

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="$RAIZ/services/gateway/certs"
KEY="$DIR/polebet.key"
CRT="$DIR/polebet.crt"

FORZAR=0
case "${1:-}" in
  --force|-f) FORZAR=1 ;;
  "") ;;
  *) echo "uso: $0 [--force]" >&2; exit 2 ;;
esac

command -v openssl >/dev/null || { echo "falta openssl" >&2; exit 1; }

if [[ -e "$KEY" || -e "$CRT" ]] && [[ $FORZAR -eq 0 ]]; then
  echo "Ya existen certificados en $DIR (usa --force para regenerarlos)."
  openssl x509 -in "$CRT" -noout -subject -enddate -ext subjectAltName
  exit 0
fi

mkdir -p "$DIR"
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 365 \
  -keyout "$KEY" -out "$CRT" \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,DNS:gateway,IP:127.0.0.1" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth" 2>/dev/null
chmod 600 "$KEY"
chmod 644 "$CRT"

echo "Certificado generado en $DIR:"
openssl x509 -in "$CRT" -noout -subject -enddate -ext subjectAltName
