#!/usr/bin/env bash
# Captura la evidencia "despues" del escenario 4 (T3-05). Se corre desde la
# raiz del repo con el stack levantado y las llaves generadas:
#   bash documentation/prototipo3/escenario-4-auth-servicios/despues/capturar-despues.sh <salidas> [filtro]
# Con [filtro] (regex sobre el nombre) solo repite esas pruebas, p. ej. '^13-'.
# Cada prueba guarda en <salidas>/NN-nombre.txt el comando exacto ($ ...) y lo
# que respondio. Usa los jugadores de prueba de evidencia-antes.md.
# Ojo: R3 hace una compra real a nombre de la victima, y la prueba 16 espera 61 s.
set -u
SALIDA="$1"
SOLO="${2:-.}"
mkdir -p "$SALIDA"

# Variables que usan los comandos (se exportan para que las vea bash -c).
export CA=services/gateway/certs/ca.crt
export H=documentation/prototipo3/escenario-4-auth-servicios/despues/herramientas_jwt.py
export VICTIMA=f0c2d24e-4a1a-4266-9145-ebea0867501f
TMP=$(mktemp -d)
export JAR_V="$TMP/victima.jar" JAR_A="$TMP/atacante.jar" TMP
trap 'rm -rf "$TMP"' EXIT

# cookie_de JAR: la cookie de sesion guardada en un cookie jar de curl.
cookie_de() { awk '$6 == "session" { print $7 }' "$1"; }
# token_de JAR: la identidad firmada que Usuarios entrega para esa cookie. Se
# pide desde el Gateway, igual que su auth_request: es el token que viaja de
# verdad por la red interna (el que un atacante podria copiar).
token_de() {
  docker exec arqui_gateway curl -s -D - -o /dev/null -H "Cookie: session=$(cookie_de "$1")" \
    http://usuarios:8001/internal/auth | tr -d '\r' | awk -F': ' 'tolower($1) == "x-user-token" { print $2 }'
}
export -f cookie_de token_de

# cap NOMBRE < comando: ejecuta el comando (leido de la entrada) y guarda
# comando + salida.
cap() {
  local nombre="$1" cmd
  cmd=$(cat)
  [[ "$nombre" =~ $SOLO ]] || return 0
  {
    printf '$ %s\n' "$cmd"
    bash -c "$cmd" 2>&1
    printf '\n'
  } >"$SALIDA/$nombre.txt"
  echo "== $nombre"; cat "$SALIDA/$nombre.txt"
}

# Sesiones de la victima y del atacante (cada uno con su propia contrasena).
curl -s --cacert $CA -c "$JAR_V" -o /dev/null https://localhost/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"e4.victima@polebet.co","password":"Clave-e4-victima-123"}'
curl -s --cacert $CA -c "$JAR_A" -o /dev/null https://localhost/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"e4.atacante@polebet.co","password":"Clave-e4-atacante-123"}'

# ---------------------------------------------------------------------------
# A. Los mismos ataques del "antes"
# ---------------------------------------------------------------------------
cap 01-gateway-publico-borra-cabecera <<'CMD'
curl -s -o /dev/null -w 'HTTP %{http_code}\n' --cacert $CA -H "X-User-Id: $VICTIMA" -H "X-User-Rol: player" https://localhost/api/billetera
CMD

cap 02-gateway-sin-cabecera <<'CMD'
docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' http://orquestador:8081/billetera
CMD

cap 03-gateway-suplanta-billetera <<'CMD'
docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -H "X-User-Id: $VICTIMA" -H "X-User-Rol: player" http://orquestador:8081/billetera
CMD

cap 04-gateway-suplanta-pagina <<'CMD'
docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -H "X-User-Id: $VICTIMA" -H "X-User-Rol: player" http://orquestador:8081/pagina-usuario
CMD

cap 05-gateway-suplanta-compra <<'CMD'
docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -X POST -H 'Content-Type: application/json' -H 'Idempotency-Key: e4-ataque-2' -H "X-User-Id: $VICTIMA" -H "X-User-Rol: player" -d '{"monto_cop":5000}' http://orquestador:8081/billetera/compras
CMD

cap 06-pagos-suplanta-billetera <<'CMD'
docker exec arqui_pagos python -c "import http.client as h; c=h.HTTPConnection('orquestador', 8081); c.request('GET', '/billetera', headers={'X-User-Id': '$VICTIMA', 'X-User-Rol': 'player'}); r=c.getresponse(); print(r.status, r.read().decode())"
CMD

cap 07-ws-motor-sin-cabecera <<'CMD'
docker exec arqui_gateway curl -s -i --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: http://localhost' http://motor:8080/ws | head -1
CMD

cap 08-ws-motor-suplanta <<'CMD'
docker exec arqui_gateway curl -s -i --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: http://localhost' -H "X-User-Id: $VICTIMA" http://motor:8080/ws | head -1
CMD

cap 09-jwt-secret-solo-en-usuarios <<'CMD'
for c in arqui_usuarios arqui_motor; do printf '%-16s ' $c; docker exec $c sh -c 'if [ -n "$JWT_SECRET" ]; then printf %s "$JWT_SECRET" | sha256sum; else echo "(no tiene JWT_SECRET)"; fi'; done
CMD

cap 10-cookie-fabricada-con-secreto-del-motor <<'CMD'
SECRETO=$(docker exec arqui_motor printenv JWT_SECRET); echo "secreto sacado del motor: '${SECRETO}'"; COOKIE=$(python3 -I -c "import base64,hmac,hashlib,json,sys,time; b=lambda d: base64.urlsafe_b64encode(d).rstrip(b'=').decode(); h=b(json.dumps({'alg':'HS256','typ':'JWT'}).encode()); p=b(json.dumps({'sub':'$VICTIMA','rol':'player','iat':int(time.time()),'exp':int(time.time())+3600}).encode()); s=b(hmac.new(sys.argv[1].encode(), (h+'.'+p).encode(), hashlib.sha256).digest()); print(h+'.'+p+'.'+s)" "$SECRETO"); curl -s -w '\nHTTP %{http_code}\n' --cacert $CA -b "session=$COOKIE" https://localhost/api/billetera
CMD

# ---------------------------------------------------------------------------
# B. Donde esta cada llave
# ---------------------------------------------------------------------------
cap 11-donde-esta-cada-llave <<'CMD'
for c in arqui_usuarios arqui_orquestador arqui_motor arqui_gateway arqui_pagos; do echo "--- $c"; docker exec $c sh -c 'ls /run/secrets/ 2>/dev/null | sed "s/^/privada: \/run\/secrets\//"; ls /etc/polebet/ 2>/dev/null | sed "s/^/publica: \/etc\/polebet\//"; true'; done
CMD

# ---------------------------------------------------------------------------
# C. Ataques contra la firma (necesitan un token real, que antes no existia)
# ---------------------------------------------------------------------------
cap 12-token-propio-con-sub-cambiado <<'CMD'
PROPIO=$(token_de "$JAR_A"); echo "--- con su propio token, el atacante ve SU billetera:"; docker exec arqui_gateway curl -s -w '  HTTP %{http_code}\n' -H "X-User-Token: $PROPIO" http://orquestador:8081/billetera; echo "--- el mismo token con el sub de la victima:"; ALTERADO=$(python3 -I $H cambiar-sub "$PROPIO" $VICTIMA); docker exec arqui_gateway curl -s -w '  HTTP %{http_code}\n' -H "X-User-Token: $ALTERADO" http://orquestador:8081/billetera
CMD

cap 13-token-firmado-con-otra-llave <<'CMD'
openssl genpkey -algorithm ed25519 -out "$TMP/atacante.key"; CUERPO=$(python3 -I $H cuerpo $VICTIMA); printf %s "$CUERPO" > "$TMP/cuerpo"; FIRMA=$(openssl pkeyutl -sign -inkey "$TMP/atacante.key" -rawin -in "$TMP/cuerpo" | python3 -I $H b64url); echo "token del atacante: $CUERPO.$FIRMA" | cut -c1-110; docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -H "X-User-Token: $CUERPO.$FIRMA" http://orquestador:8081/billetera
CMD

cap 14-token-sin-firma-alg-none <<'CMD'
docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -H "X-User-Token: $(python3 -I $H none $VICTIMA)" http://orquestador:8081/billetera
CMD

cap 15-token-hs256-con-secreto <<'CMD'
SECRETO=$(docker exec arqui_usuarios printenv JWT_SECRET); docker exec arqui_gateway curl -s -w '\nHTTP %{http_code}\n' -H "X-User-Token: $(python3 -I $H hs256 $VICTIMA "$SECRETO")" http://orquestador:8081/billetera
CMD

cap 16-token-real-reusado-despues-de-61s <<'CMD'
COPIADO=$(token_de "$JAR_V"); echo "--- recien copiado:"; docker exec arqui_gateway curl -s -w '  HTTP %{http_code}\n' -H "X-User-Token: $COPIADO" http://orquestador:8081/billetera; sleep 61; echo "--- 61 s despues:"; docker exec arqui_gateway curl -s -w '  HTTP %{http_code}\n' -H "X-User-Token: $COPIADO" http://orquestador:8081/billetera
CMD

cap 17-ws-motor-token-alterado <<'CMD'
ALTERADO=$(python3 -I $H cambiar-sub "$(token_de "$JAR_A")" $VICTIMA); docker exec arqui_gateway curl -s -i --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: http://localhost' -H "X-User-Token: $ALTERADO" http://motor:8080/ws | head -1
CMD

# ---------------------------------------------------------------------------
# D. Tokens de servicio: uno por par llamador -> destino (segunda parte)
# Los tokens se muestran solo como los primeros 12 caracteres de su sha256.
# ---------------------------------------------------------------------------
cap 18-que-token-tiene-cada-contenedor <<'CMD'
for c in arqui_orquestador arqui_usuarios arqui_pagos arqui_pagos_consumer arqui_motor; do echo "--- $c"; docker exec $c sh -c 'env | grep -E "^(TOKEN_|INTERNAL_API_TOKEN)" | sort | while IFS="=" read -r n v; do printf "  %-24s sha256=%s\n" "$n" "$(printf %s "$v" | sha256sum | cut -c1-12)"; done; env | grep -qE "^(TOKEN_|INTERNAL_API_TOKEN)" || echo "  (ningun token de servicio)"'; done
CMD

cap 19-motor-llama-usuarios <<'CMD'
for v in TOKEN_DESDE_ORQUESTADOR TOKEN_HACIA_PAGOS; do printf '%-24s ' $v; echo "$(docker exec arqui_motor sh -c "wget -qO- --header \"X-Internal-Service-Token: \$$v\" http://usuarios:8001/internal/users/$VICTIMA 2>&1")"; done
CMD

cap 20-pagos-llama-usuarios <<'CMD'
docker exec arqui_pagos python -c "import os,http.client as h
for v in ('TOKEN_DESDE_ORQUESTADOR','TOKEN_DESDE_MOTOR'): c=h.HTTPConnection('usuarios',8001); c.request('GET','/internal/users/$VICTIMA',headers={'X-Internal-Service-Token':os.environ[v]}); r=c.getresponse(); print(f'{v:<24}', r.status, r.read().decode())"
CMD

cap 21-pagos-llama-motor <<'CMD'
docker exec arqui_pagos python -c "import os,http.client as h
for v in ('TOKEN_DESDE_ORQUESTADOR','TOKEN_DESDE_MOTOR'): c=h.HTTPConnection('motor',8080); c.request('GET','/internal/posiciones',headers={'X-Internal-Service-Token':os.environ[v]}); r=c.getresponse(); print(f'{v:<24}', r.status, r.read().decode().strip())"
CMD

cap 22-usuarios-llama-pagos <<'CMD'
docker exec arqui_usuarios python -c "import os,http.client as h
c=h.HTTPConnection('pagos',8000); c.request('GET','/internal/wallets/$VICTIMA',headers={'X-Internal-Service-Token':os.environ['TOKEN_DESDE_ORQUESTADOR']}); r=c.getresponse(); print('TOKEN_DESDE_ORQUESTADOR ', r.status, r.read().decode())"
CMD

cap 23-orquestador-token-de-otro-destino <<'CMD'
for v in TOKEN_HACIA_PAGOS TOKEN_HACIA_MOTOR TOKEN_HACIA_USUARIOS; do printf '%-21s -> usuarios  ' $v; echo "$(docker exec arqui_orquestador sh -c "wget -qO- --header \"X-Internal-Service-Token: \$$v\" http://usuarios:8001/internal/users/$VICTIMA 2>&1")"; done
CMD

cap 24-llamadas-legitimas <<'CMD'
echo "--- Motor -> Pagos con TOKEN_HACIA_PAGOS:"; docker exec arqui_motor sh -c 'wget -qO- --header "X-Internal-Service-Token: $TOKEN_HACIA_PAGOS" http://pagos:8000/internal/wallets/'$VICTIMA; echo; echo "--- Orquestador -> Pagos con TOKEN_HACIA_PAGOS:"; docker exec arqui_orquestador sh -c 'wget -qO- --header "X-Internal-Service-Token: $TOKEN_HACIA_PAGOS" http://pagos:8000/internal/wallets/'$VICTIMA; echo; echo "--- Orquestador -> Motor (GET /internal/dinamica-actual):"; for v in TOKEN_HACIA_PAGOS TOKEN_HACIA_MOTOR; do printf '  con %-18s ' $v; echo "$(docker exec arqui_orquestador sh -c "wget -qO- --header \"X-Internal-Service-Token: \$$v\" http://motor:8080/internal/dinamica-actual 2>&1")"; done
CMD

cap 25-tokens-repetidos-no-arranca <<'CMD'
docker run --rm -e USUARIOS_BASE_URL=http://usuarios:8001 -e PAGOS_BASE_URL=http://pagos:8000 -e MOTOR_BASE_URL=http://motor:8080 -e IDENTIDAD_LLAVE_PUBLICA=/x.pub -e TOKEN_HACIA_USUARIOS=un-token-de-prueba-de-32-caracteres-aaaa -e TOKEN_HACIA_PAGOS=el-mismo-token-copiado-en-dos-destinos-xx -e TOKEN_HACIA_MOTOR=el-mismo-token-copiado-en-dos-destinos-xx arquisoft-prototipo2-orquestador 2>&1; echo "codigo de salida: $?"
CMD

# ---------------------------------------------------------------------------
# R. Regresion: la app sigue funcionando por el Gateway
# ---------------------------------------------------------------------------
cap R1-login <<'CMD'
curl -s -w '\nHTTP %{http_code}\n' --cacert $CA -c "$JAR_V" https://localhost/api/auth/login -H 'Content-Type: application/json' -d '{"email":"e4.victima@polebet.co","password":"Clave-e4-victima-123"}'
CMD

cap R2-billetera <<'CMD'
curl -s -w '\nHTTP %{http_code}\n' --cacert $CA -b "$JAR_V" https://localhost/api/billetera
CMD

cap R3-compra <<'CMD'
curl -s -w '\nHTTP %{http_code}\n' --cacert $CA -b "$JAR_V" https://localhost/api/billetera/compras -H 'Content-Type: application/json' -H "Idempotency-Key: e4-regresion-$(date +%s)" -d '{"monto_cop":10000}'
CMD

cap R4-pagina-usuario <<'CMD'
curl -s -o /dev/null -w 'HTTP %{http_code}\n' --cacert $CA -b "$JAR_V" https://localhost/api/pagina-usuario
CMD

cap R5-apuesta-pasa-la-autenticacion <<'CMD'
curl -s -w '\nHTTP %{http_code}\n' --cacert $CA -b "$JAR_V" https://localhost/api/apuestas -H 'Content-Type: application/json' -H "Idempotency-Key: e4-apuesta-$(date +%s)" -d '{"mercado_id":"0f8a6121-807b-5ef8-b1a9-b3651c545099","opcion":"LEC","monto":5}'
CMD

cap R6-websocket-por-el-gateway <<'CMD'
curl -s -i --max-time 3 --cacert $CA -b "$JAR_V" -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: https://localhost' https://localhost/api/ws | head -1; sleep 1; docker logs arqui_motor 2>&1 | grep "cliente WS" | tail -2
CMD

cap R7-admin-por-el-gateway <<'CMD'
set -a; . ./.env; set +a; TOKEN=$(curl -s --cacert $CA https://localhost/api/admin/login -H 'Content-Type: application/json' -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | python3 -I -c "import json,sys; print(json.load(sys.stdin)['token'])"); curl -s -o /dev/null -w 'GET /api/admin/carreras -> HTTP %{http_code}\n' --cacert $CA -H "Authorization: Bearer $TOKEN" https://localhost/api/admin/carreras
CMD

cap R8-registro-publico <<'CMD'
curl -s -w '\nHTTP %{http_code}\n' --cacert $CA https://localhost/api/registro -H 'Content-Type: application/json' -d "{\"nombre\":\"E4 regresion\",\"email\":\"e4.regresion.$(date +%s)@polebet.co\",\"contrasena\":\"Clave-e4-regresion-123\"}"
CMD
