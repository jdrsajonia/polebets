# Usuarios (stub base — T2-04)

Esqueleto del servicio **Usuarios** (Python / FastAPI). Dueño: **Misael** (HU2-01, HU2-02, HU2-03).

Hoy solo responde `GET /health` para que el contenedor arranque y todo el stack
levante con un solo comando. La lógica real se construye encima de esta base.

## Qué implementar aquí

- Cuentas, contraseñas (bcrypt/Argon2), roles y emisión del **JWT en cookie HttpOnly** (HU2-02).
- `POST /auth/login`, `POST /auth/logout` (el Gateway enruta login/logout aquí, T2-01).
- `GET /internal/auth` para el `auth_request` del Gateway: valida la cookie y devuelve
  los headers `X-User-Id` y `X-User-Rol` (RNF-17).
- Rutas internas del contrato del Orquestador (`documentation/orquestador-api.md`, §5):
  - `POST /internal/users {id, nombre, email, contrasena}`
  - `GET /internal/users/{id}`

## Datos

Dueño de la PostgreSQL **Usuarios** (tabla `users`, sin `balance` ni `points` — eso vive en Pagos).
Variable de entorno `DATABASE_URL` apunta al contenedor `postgres-usuarios` (ver docker-compose.yml).

## Puerto

Interno **8001** (así lo espera `USUARIOS_BASE_URL` en el contrato). No se publica al host.
