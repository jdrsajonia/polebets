# Orquestador (Go)

Coordina los flujos que tocan varios servicios: registro, página del usuario (SSR), compra de tokens, calendario, inscripción y apuesta. Llama a Usuarios, Pagos y Motor en orden, responde un solo resultado y reintenta lo idempotente cuando un paso falla. Solo recibe tráfico del API Gateway.

No valida el JWT (lo hace el Gateway), no tiene base de datos y ningún otro servicio lo llama. El contrato completo de las rutas está en [`documentation/orquestador-api.md`](../../documentation/orquestador-api.md).

## Estructura

```
main.go           # solo arma las piezas, arranca el servidor y lo apaga limpio (Ctrl+C / SIGTERM)
internal/config   # variables de entorno (las obligatorias no tienen valor por defecto)
internal/httpx    # EscribirJSON
internal/health   # GET /health
internal/billetera # GET /billetera, POST /billetera/compras (HU2-04)
internal/registro  # POST /registro, público: billetera en Pagos y luego cuenta en Usuarios (HU2-01)
internal/pagina    # GET /pagina-usuario: Usuarios + Pagos + Motor en paralelo para la página SSR (HU2-03)
internal/errores  # formato {error, codigo} y la tabla de errores del contrato
internal/sesion   # middleware: verifica la identidad firmada (X-User-Token) → 401 / 403
internal/sesion/sesiontest # firma identidades de prueba para los tests de los módulos
internal/traza    # X-Request-Id de cada petición
internal/interno  # cliente HTTP hacia Usuarios, Pagos y Motor (token, timeout, reintentos)
```

Cada flujo va en su propio paquete de `internal/` con una función `Register(mux, ...)` que se llama desde `main.go`. Las rutas de jugador se envuelven con el middleware de sesión; las públicas (`/health`, `/registro`) no:

```go
mux.Handle("POST /apuestas", sesion.ExigirJugador(http.HandlerFunc(apostar)))
```

## Variables de entorno

| Variable | Obligatoria | Uso |
| --- | --- | --- |
| `USUARIOS_BASE_URL` | sí | URL de Usuarios, p. ej. `http://usuarios:8001` |
| `PAGOS_BASE_URL` | sí | URL de Pagos, p. ej. `http://pagos:8000` |
| `MOTOR_BASE_URL` | sí | URL del Motor, p. ej. `http://motor:8080` |
| `TOKEN_HACIA_USUARIOS` | sí | Token de servicio para Usuarios, mínimo 32 caracteres |
| `TOKEN_HACIA_PAGOS` | sí | Token de servicio para Pagos; distinto de los otros dos |
| `TOKEN_HACIA_MOTOR` | sí | Token de servicio para el Motor; distinto de los otros dos |
| `IDENTIDAD_LLAVE_PUBLICA` | sí | Llave pública de Usuarios para verificar `X-User-Token` |
| `HTTP_PORT` | no | Puerto de escucha; por defecto `8081` |

Si falta una obligatoria, el servicio no arranca y el error dice cuáles faltan.

## Correrlo

Con Go 1.25 o superior, desde `services/orquestador`:

```bash
export USUARIOS_BASE_URL=http://localhost:8001
export PAGOS_BASE_URL=http://localhost:8000
export MOTOR_BASE_URL=http://localhost:8080
# Cada token debe ser el mismo que tenga su destino (TOKEN_DESDE_ORQUESTADOR allá)
export TOKEN_HACIA_USUARIOS=$(openssl rand -hex 32)
export TOKEN_HACIA_PAGOS=$(openssl rand -hex 32)
export TOKEN_HACIA_MOTOR=$(openssl rand -hex 32)
export IDENTIDAD_LLAVE_PUBLICA=../usuarios/llaves/identidad.pub
go run .
```

Pruebas:

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

## Probarlo con curl

```bash
curl -i localhost:8081/health
# HTTP/1.1 200 OK
# X-Request-Id: 3f0c…        (generado; si mandas -H "X-Request-Id: algo", vuelve el mismo)
# {"estado":"ok"}
```

Las rutas de jugador exigen `X-User-Token`, la identidad que firma Usuarios con su llave privada (escenario 4). Escribir `X-User-Id` a mano ya no sirve, así que se prueban por el Gateway con la cookie de una sesión:

```bash
curl -s --cacert services/gateway/certs/ca.crt -c sesion.jar https://localhost/api/auth/login \
  -H "Content-Type: application/json" -d '{"email":"ana@correo.co","password":"clave-segura"}'
curl -i --cacert services/gateway/certs/ca.crt -b sesion.jar https://localhost/api/eventos
```

En los tests, `sesiontest.Identificar(req, id, rol)` pone una identidad firmada con una llave de prueba.

El registro es público (sin headers):

```bash
curl -i localhost:8081/registro -H "Content-Type: application/json" \
  -d '{"nombre":"Ana","email":"ana@correo.co","contrasena":"clave-segura"}'
# 201 {"usuario":{"id":"…","nombre":"Ana","email":"ana@correo.co","rol":"player"}}
# 409 correo_registrado · 400 cuerpo_invalido · 503 servicio_no_disponible
```

Sin identidad válida la respuesta es `401 {"error":"No hay sesión","codigo":"sin_sesion"}`, y con rol `admin` es `403 solo_jugadores`. Las rutas de cada flujo (`/eventos`, `/apuestas`, …) se agregan una por una; mientras no exista una ruta, el servidor responde 404.
