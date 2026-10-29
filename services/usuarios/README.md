# Usuarios (base — T2-04, T2-03)

Esqueleto del servicio **Usuarios** (Python / FastAPI). Dueño: **Misael** (HU2-01, HU2-02, HU2-03).

Hoy responde `GET /health` y crea la tabla `users`. La lógica real se construye
encima de esta base.

## Qué implementar aquí

- Cuentas, contraseñas (bcrypt/Argon2), roles y emisión del **JWT en cookie HttpOnly** (HU2-02).
- `POST /auth/login`, `POST /auth/logout` (el Gateway enruta login/logout aquí, T2-01).
- `GET /internal/auth` para el `auth_request` del Gateway: valida la cookie y devuelve
  los headers `X-User-Id` y `X-User-Rol` (RNF-17).
- Rutas internas del contrato del Orquestador (`documentation/orquestador-api.md`, §5):
  - `POST /internal/users {id, nombre, email, contrasena}`
  - `GET /internal/users/{id}`

## Datos (T2-03)

Dueño de la PostgreSQL **Usuarios**. La tabla `users` ya existe (`app/models.py`)
y se crea al arrancar: `id, email (único), name, password_hash, role (player | admin), created_at`,
sin `balance` ni `points` — el saldo vive en `wallets`, en Pagos.

- `id` no tiene default en Python: en el registro lo genera el Orquestador y Usuarios lo guarda tal cual (HU2-01).
- `DATABASE_URL` apunta al contenedor `postgres-usuarios` (ver docker-compose.yml).
- `/health` responde 503 si PostgreSQL no responde.
- Pruebas: `pip install -r requirements-dev.txt` y
  `DATABASE_URL=postgresql+psycopg://arqui:arqui_pass@localhost:55432/usuarios_db pytest`
  (PostgreSQL aparte, como en `services/pagos/README.md`).

## Puerto

Interno **8001** (así lo espera `USUARIOS_BASE_URL` en el contrato). No se publica al host.
