# API Gateway (T2-01)

**API Gateway** (Nginx). Dueño: **Tomás**. Única entrada web de los jugadores y
uno de los dos únicos puertos publicados al host (80).

## Qué hace

Toda la configuración está en `nginx.conf`. Enruta por prefijo:

| Ruta pública   | Destino      | Sesión        |
| -------------- | ------------ | ------------- |
| `/api/auth/`   | Usuarios     | no (login, logout, me) |
| `/api/registro` | Orquestador | no (registro, HU2-01) |
| `/api/ws`      | Motor        | protegida (WebSocket) |
| `/api/`        | Orquestador  | protegida (se quita el prefijo `/api`) |
| `/`            | Front        | no (SPA y assets) |
| `/health`      | —            | healthcheck del propio Gateway |

En las rutas protegidas:

- Hace `auth_request` a Usuarios (`GET /internal/auth`); sin sesión válida → 401.
- **Neutraliza** `X-User-Id` / `X-User-Rol` en toda petición y reinyecta solo los
  que devuelve Usuarios, para que el cliente no pueda suplantar a otro (RNF-17).
- El WebSocket usa `proxy_read_timeout 1h`, así no se corta a los 60 s.

## Redes

Vive en **borde** (de cara al host) e **interna** (para alcanzar Usuarios,
Orquestador, Motor y Front). No toca ninguna base de datos.

## Sesión (HU2-02)

`GET /internal/auth` de Usuarios responde `204` con `X-User-Id` y `X-User-Rol`
(el rol sale de la base, no del token) o `401`. El mock de `/_auth` que trae
`nginx.conf` comentado ya no hace falta.

## Pendiente

- El Motor todavía valida el JWT de la cookie por su cuenta al abrir el
  WebSocket; HU2-02 pide que identifique al jugador por `X-User-Id`. Como la
  cookie también le llega por `/api/ws`, funciona igual mientras tanto.
