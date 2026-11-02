# Usuarios (HU2-01, HU2-02)

Servicio **Usuarios** (Python / FastAPI). Dueño: **Misael**. Es el dueño de la
PostgreSQL **Usuarios** (tabla `users`) y el único que emite el JWT de sesión.

## Rutas

| Ruta | Quién la llama | Qué hace |
| --- | --- | --- |
| `POST /auth/login` | Front, vía Gateway (`/api/auth/login`); Admin (RF-21) | Valida correo y contraseña y deja el JWT en la cookie `session` (HttpOnly, SameSite=Lax). 401 «Correo o contraseña incorrectos» |
| `POST /auth/logout` | Front, vía Gateway | Borra la cookie. Siempre 204 |
| `GET /auth/me` | Front, vía Gateway | El usuario de la cookie, o 401. El front lo usa al recargar |
| `GET /internal/auth` | Gateway (`auth_request`) | 204 + `X-User-Id` y `X-User-Rol` si la cookie es válida; 401 si no |
| `POST /internal/users` | Orquestador (registro, HU2-01) | Crea el jugador con el `id` que manda el Orquestador. 201 · 200 si ese `id` ya existía con el mismo correo · 409 correo repetido · 422 datos inválidos |
| `GET /internal/users/{id}` | Orquestador (página SSR, HU2-03) | `{id, nombre, email, rol}` o 404 |
| `GET /health` | Docker | 200, o 503 si PostgreSQL no responde |

Las rutas `/internal/users*` exigen `X-Internal-Service-Token`. `POST
/internal/users` acepta `Idempotency-Key: user:{id}` para que el cliente del
Orquestador pueda reintentarla; si llega otra clave, 400.

`GET /internal/auth` no pide token: la llama Nginx, que no lee el `.env`, y
Usuarios no está publicado al host, así que solo el Gateway la alcanza.

## Decisiones

- **JWT igual al del prototipo 1:** HS256 con `JWT_SECRET`, claims `sub`
  (uuid), `rol` (`player` | `admin`) y `exp`, en la cookie `session`. Así el
  Motor, que todavía valida el WebSocket por su cuenta, sigue funcionando.
- **El rol sale de la base, no del token**, en `/internal/auth` y `/auth/me`:
  un usuario borrado pierde la sesión y un token alterado no sube de rol.
- **Mensaje único** para cualquier credencial inválida, y un hash señuelo para
  que un correo inexistente tarde lo mismo que una clave mala (no se puede
  averiguar qué correos existen).
- **Contraseñas en bcrypt**, nunca en texto plano (RNF-06).
- **Correo case-insensitive:** se guarda en minúsculas (`normalizar_email`).
- **Login detrás de una interfaz** (`ProveedorIdentidad`), para pasar a OAuth
  sin tocar los routers.
- **Administrador inicial (RF-21):** con la separación de bases la tabla
  `users` pasó de Pagos a Usuarios, y con ella la creación del admin. Si
  `ADMIN_EMAIL` y `ADMIN_PASSWORD` están definidos, Usuarios lo crea al
  arrancar; si ya existe no lo toca, y si ese correo es de un jugador no
  arranca.

## Estructura

```
app/main.py                      # arranque, routers y /health
app/config.py                    # variables de entorno (JWT_SECRET e INTERNAL_API_TOKEN obligatorias)
app/core/security.py             # bcrypt y JWT
app/core/session.py              # cookie de sesión
app/core/internal_auth.py        # token de servicio
app/deps.py                      # sesión de BD, token de servicio, usuario de la cookie
app/routers/auth.py              # /auth/*
app/routers/internal.py          # /internal/*
app/services/auth.py             # login (ProveedorIdentidad)
app/services/usuarios.py         # crear y leer cuentas (registro idempotente)
app/services/admin_bootstrap.py  # administrador inicial
```

## Pruebas

Contra una PostgreSQL aparte (como en `services/pagos/README.md`):

```bash
pip install -r requirements-dev.txt
DATABASE_URL=postgresql+psycopg://arqui:arqui_pass@localhost:55432/usuarios_db pytest
```

Sin PostgreSQL, las pruebas se saltan (skip).

## Puerto

Interno **8001** (así lo espera `USUARIOS_BASE_URL` en el contrato). No se publica al host.
