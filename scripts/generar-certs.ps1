# Genera el certificado autofirmado del Gateway (escenarios 1 y 2, P3).
#
#   .\scripts\generar-certs.ps1          # no pisa uno que ya exista
#   .\scripts\generar-certs.ps1 -Force   # lo regenera
#
# Deja services\gateway\certs\polebet.key y polebet.crt (en .gitignore).
# SAN: localhost (navegador y CLI desde el host), gateway (la CLI dentro de
# Docker llama a https://gateway) y 127.0.0.1.
# CA:FALSE + serverAuth: es un certificado de servidor, no una CA. Por defecto
# `openssl req -x509` pone CA:TRUE y rustls (la CLI) lo rechaza con
# CaUsedAsEndEntity; curl y los navegadores no se quejan, pero la CLI sí.
# Necesita openssl en el PATH (viene con Git for Windows:
# C:\Program Files\Git\usr\bin). Después: docker compose restart gateway
param([switch]$Force)
$ErrorActionPreference = "Stop"

$raiz = Split-Path -Parent $PSScriptRoot
$dir = Join-Path $raiz "services\gateway\certs"
$key = Join-Path $dir "polebet.key"
$crt = Join-Path $dir "polebet.crt"

if (-not (Get-Command openssl -ErrorAction SilentlyContinue)) {
    $gitOpenssl = "C:\Program Files\Git\usr\bin"
    if (Test-Path (Join-Path $gitOpenssl "openssl.exe")) {
        $env:PATH = "$gitOpenssl;$env:PATH"
    } else {
        throw "falta openssl (instala Git for Windows o agrega openssl al PATH)"
    }
}

if (((Test-Path $key) -or (Test-Path $crt)) -and -not $Force) {
    Write-Host "Ya existen certificados en $dir (usa -Force para regenerarlos)."
    & openssl x509 -in $crt -noout -subject -enddate -ext subjectAltName
    exit 0
}

New-Item -ItemType Directory -Force -Path $dir | Out-Null
& openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 365 `
    -keyout $key -out $crt `
    -subj "/CN=localhost" `
    -addext "subjectAltName=DNS:localhost,DNS:gateway,IP:127.0.0.1" `
    -addext "basicConstraints=critical,CA:FALSE" `
    -addext "keyUsage=critical,digitalSignature,keyEncipherment" `
    -addext "extendedKeyUsage=serverAuth" 2>$null
if ($LASTEXITCODE -ne 0) { throw "openssl falló ($LASTEXITCODE)" }

Write-Host "Certificado generado en ${dir}:"
& openssl x509 -in $crt -noout -subject -enddate -ext subjectAltName
