# Admin (Python / FastAPI) — HU2-17 (login), HU2-18, HU2-19 y HU2-23

Servicio que atiende a la CLI del administrador. **No está publicado al host**:
escucha en el puerto 8003 solo dentro de Docker y se alcanza a través del API Gateway en
**`https://localhost/api/admin/`** (escenario 2 del P3, reverse proxy; red interna
`gw_admin`, que comparte solo con el Gateway). El Gateway quita el prefijo:
`/api/admin/login` → `/login`. No publica `/docs`, `/redoc` ni `/openapi.json`; el
contrato es este README. No toca dinero ni apuestas: habla con Pagos y
con el Ingestor **solo por el Broker** (RabbitMQ), en petición-respuesta.

Cada petición declara una cola temporal exclusiva, la manda en `reply_to` junto con un
`correlation_id` (propiedades AMQP) y espera su respuesta hasta **10 s** (RF-30). Si no
llega, responde **504** «la consulta tardó demasiado». El mensaje expira a los 10 s, así
una orden vieja no se procesa después de que la CLI recibió el aviso. Si el Broker no
está, responde **503**.

`RABBITMQ_URL` y `USUARIOS_BASE_URL` son obligatorias: sin ellas Admin no arranca.

## Sesión del administrador (HU2-17, RF-21)

Solo entra el rol `admin`. Admin no guarda usuarios ni conoce el `JWT_SECRET`: todo lo
pregunta a Usuarios por la red interna (`app/sesion.py`).

1. `POST /login {email, password}` → Admin llama a `POST /auth/login` de Usuarios. Si el
   rol no es `admin` responde **403** y no entrega el token; si lo es, responde
   `{status, token, usuario}`.
2. La CLI manda ese token en `Authorization: Bearer <token>` en las demás rutas. Admin lo
   valida en cada petición con `GET /internal/auth` de Usuarios (la misma subpetición del
   Gateway): el rol sale de la base, no del token.

| Caso | Respuesta |
| --- | --- |
| Sin `Authorization`, token mal formado o sesión vencida | **401** (la CLI vuelve a pedir credenciales) |
| Credenciales incorrectas en `/login` | **401** «Correo o contraseña incorrectos» |
| Rol `player` | **403** «Solo los administradores pueden usar la CLI» |
| Usuarios no responde | **503** |

El token dura lo que el JWT de Usuarios (`JWT_EXPIRE_MINUTES`, 120 por defecto). No hay
logout en Admin: la CLI simplemente descarta el token.

## Rutas

Todas menos `/health` y `/login` exigen `Authorization: Bearer <token>` de un admin.

| Ruta | Cola | Respuestas |
| --- | --- | --- |
| `POST /login` | — (Usuarios por REST) | 200 `{token, usuario}`, 401, 403 o 503 (ver arriba) |
| `GET /carreras` | `ingestor.consultas` (`list_races`) | 200 con las carreras ya descargadas en MongoDB |
| `POST /simulacion` | `ingestor.commands` (`start_simulation`) | 200 `{simulacion_id, hora_largada}`, 409 si ya hay una programada o en curso, 404 si la carrera no está descargada |
| `POST /consultas` | `admin.consultas` (consumidor de Pagos) | 200 con `data`, `filtros_aplicados` y `filtros_no_aplicados` |

Con el stack levantado. `--cacert` verifica el certificado del Gateway con su CA local
(`services/gateway/certs/ca.crt`, de `scripts/generar-certs.sh`).

```bash
# Login con el admin inicial del .env (ADMIN_EMAIL / ADMIN_PASSWORD)
A=https://localhost/api/admin
CA=services/gateway/certs/ca.crt
TOKEN=$(curl -s --cacert $CA -X POST $A/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@arqui.local","password":"cambiar-esta-clave-solo-dev"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')
AUTH="Authorization: Bearer $TOKEN"

curl -s --cacert $CA $A/carreras -H "$AUTH"
curl -s --cacert $CA -X POST $A/simulacion -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"race_id":"9165","minutos_anticipacion":10}'
curl -s --cacert $CA -X POST $A/consultas -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"query":"rentabilidad","params":{"tipo_apuesta":"estatica","top_n":5}}'
curl -s --cacert $CA -X POST $A/consultas -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"query":"resumen_financiero","params":{"modo":"torneo"}}'
curl -s --cacert $CA -X POST $A/consultas -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"query":"participacion","params":{}}'
```

```powershell
# PowerShell 7+
$A = "https://localhost/api/admin"
$CA = "services\gateway\certs\ca.crt"
$R = curl.exe -s --cacert $CA -X POST "$A/login" -H "Content-Type: application/json" `
  -d '{"email":"admin@arqui.local","password":"cambiar-esta-clave-solo-dev"}'
$TOKEN = ($R | ConvertFrom-Json).token
$AUTH = "Authorization: Bearer $TOKEN"

curl.exe -s --cacert $CA "$A/carreras" -H $AUTH
curl.exe -s --cacert $CA -X POST "$A/consultas" -H $AUTH -H "Content-Type: application/json" `
  -d '{"query":"resumen_financiero","params":{"modo":"torneo"}}'
```

Consultas (`query`): `rentabilidad` (HU2-21), `resumen_financiero` (HU2-20),
`horarios` y `participacion` (HU2-22).

## Filtros (HU2-23, RF-29)

Se traducen al contrato de Pagos (`services/pagos/app/schemas/consultas.py`, que rechaza
campos extra):

| Admin (`params`) | Pagos (`filtros`) |
| --- | --- |
| `evento_id` (UUID) | `event_id` |
| `fecha_inicio` | `desde` |
| `fecha_fin` | `hasta` |
| `tipo_apuesta` (`estatica` \| `dinamica`) | `kind` |
| `plantilla` | `template` |
| `top_n` (1–100) | `top_n` |
| `modo` (`torneo` \| `individual`) | `modo` |

Cada consulta usa solo los filtros que le aplican; los demás no se mandan a Pagos
y vuelven en `filtros_no_aplicados`:

| Consulta | Filtros que aplica |
| --- | --- |
| `rentabilidad` | todos menos `modo` |
| `resumen_financiero`, `horarios`, `participacion` | `evento_id`, `fecha_inicio`, `fecha_fin`, `tipo_apuesta`, `modo` |

## Pruebas

No necesitan el stack: Usuarios y el Broker se reemplazan por falsos (`tests/conftest.py`).

```bash
cd services/admin
pip install -r requirements-dev.txt
pytest
```

Sin Python instalado:

```bash
docker run --rm -v "$PWD":/app:z -w /app python:3.12-slim \
  sh -c "pip install -q -r requirements-dev.txt && pytest"
```

```powershell
docker run --rm -v "${PWD}:/app" -w /app python:3.12-slim `
  sh -c "pip install -q -r requirements-dev.txt && pytest"
```

(`:z` es para Linux con SELinux, como Fedora; en Windows y macOS no hace falta.)
