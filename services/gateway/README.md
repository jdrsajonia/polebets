# API Gateway (T2-01)

**API Gateway** (Nginx). Única entrada al sistema: la web de los jugadores y,
desde el escenario 2 del P3, también la CLI del administrador (Admin detrás del
Gateway). Es el único servicio con puertos publicados al host.

## Puertos

| Puerto | Dónde | Para qué |
| --- | --- | --- |
| **443** | host | HTTPS (TLS, escenario 1). Toda la entrada real |
| **80** | host | solo redirige a HTTPS (`301`) |
| **8080** | interno (red `borde`) | HTTP sin TLS para el SSR del Front (`GATEWAY_URL=http://gateway:8080`) y el healthcheck. No se publica |

## Archivos (T3-01: uno por responsabilidad)

| Archivo | Va a | Contenido | Escenario |
| --- | --- | --- | --- |
| `nginx.conf` | `conf.d/default.conf` (nivel http) | servers, puertos y TLS | 1 (Juan Diego) |
| `rutas.conf` | `polebet/` (dentro del server 443/8080) | rutas de los jugadores | compartido |
| `admin.conf` | `polebet/` (dentro del server 443/8080) | `/api/admin/` → Admin | 2 (Daniel) |
| `endurecimiento.conf` | `conf.d/` (nivel http) | `server_tokens off` (no revelar la versión) | 2 (Daniel) |
| `limites-zonas.conf` | `conf.d/` (nivel http) | zonas de `limit_req` / `limit_conn` | 5 (Michael) |

`certs/` (en `.gitignore`) lo llena `scripts/generar-certs.sh --forzar` (T3-06; en
Windows, desde Git Bash): una CA local (`ca.crt`, `ca.key`) que firma el certificado
del Gateway (`polebet.crt`, `polebet.key`; EC P-256, SAN `localhost`, `gateway`,
`127.0.0.1`, 365 días). Al Gateway **solo** se le montan `polebet.crt` y `polebet.key`
en `/etc/nginx/certs` (dos binds de solo lectura): `ca.key` no entra al contenedor.
Los clientes confían en `ca.crt`.

## Rutas

| Ruta pública | Destino | Sesión |
| --- | --- | --- |
| `/api/auth/` | Usuarios | no (login, logout, me) |
| `/api/registro` | Orquestador | no (registro, HU2-01) |
| `/api/ws` | Motor | protegida (WebSocket) |
| `/api/admin/` | Admin (sin el prefijo: `/api/admin/login` → `/login`) | la valida Admin (Bearer, rol admin) |
| `/api/` | Orquestador | protegida (se quita el prefijo `/api`) |
| `/` | Front | no (SPA, assets y `/mi-pagina` con SSR) |
| `/health` | — | healthcheck del propio Gateway |

`/api/admin/` gana sobre `/api/` porque Nginx elige el prefijo más largo. No
choca con `/admin`, que es una ruta de la SPA y la sirve el Front.

En las rutas protegidas de los jugadores:

- Hace `auth_request` a Usuarios (`GET /internal/auth`); sin sesión válida → 401.
- **Neutraliza** `X-User-Id`, `X-User-Rol` y `X-User-Token` en toda petición y
  reenvía solo la identidad firmada que devuelve Usuarios (`X-User-Token`), que
  el Orquestador y el Motor verifican con la llave pública (escenario 4).
- El WebSocket usa `proxy_read_timeout 1h`, así no se corta a los 60 s.

En `/api/admin/` **no** hay `auth_request`: la CLI manda
`Authorization: Bearer <token>` (no la cookie), Nginx lo reenvía tal cual y Admin
valida el token contra Usuarios en cada petición. Ver
`documentation/prototipo3/escenario-2-reverse-proxy/README.md`.

## Redes

- **borde:** entrada desde el host, el Front (SSR) y la CLI.
- **gw_backend** (interna): Usuarios, Orquestador y Motor.
- **gw_admin** (interna): solo Admin.

No toca ninguna base de datos.

## Probar

```bash
curl -s --cacert services/gateway/certs/ca.crt https://localhost/health
curl -s --cacert services/gateway/certs/ca.crt https://localhost/api/admin/health
curl -sI --cacert services/gateway/certs/ca.crt https://localhost/   # Server: nginx (sin versión)
docker compose exec gateway ls /etc/nginx/certs                      # solo polebet.crt y polebet.key
```
```powershell
curl.exe -s --cacert services\gateway\certs\ca.crt https://localhost/health
curl.exe -s --cacert services\gateway\certs\ca.crt https://localhost/api/admin/health
curl.exe -sI --cacert services\gateway\certs\ca.crt https://localhost/
docker compose exec gateway ls /etc/nginx/certs
```

## Sesión (HU2-02)

`GET /internal/auth` de Usuarios responde `204` con `X-User-Token` (identidad
firmada con Ed25519, 60 s) además de `X-User-Id` y `X-User-Rol` (el rol sale de la
base, no del token), o `401`. El Gateway reenvía solo `X-User-Token`. El mock de `/_auth` que trae
`rutas.conf` comentado ya no hace falta.

## Pendiente

- El server del 8080 incluye `admin.conf`, así que `/api/admin/` también
  responde por HTTP dentro de la red `borde`. Habría que sacarlo de ese server
  (`nginx.conf`, escenario 1).
