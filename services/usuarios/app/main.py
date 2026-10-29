"""Usuarios — STUB base (T2-04).

Esqueleto mínimo del servicio Usuarios (HU2-01/HU2-02, Misael). Solo responde
/health para que el contenedor arranque y el resto del equipo pueda levantar
todo el stack con `docker compose up` desde ya.

Qué va aquí cuando se implemente el servicio real:
  - Cuentas, contraseñas (bcrypt/Argon2), roles y emisión del JWT (HU2-02).
  - POST /auth/login, POST /auth/logout.
  - GET /internal/auth (subpetición auth_request del Gateway, T2-01).
  - POST /internal/users, GET /internal/users/{id} (contrato del Orquestador).

Puerto interno: 8001 (ver documentation/orquestador-api.md, USUARIOS_BASE_URL).
"""
from fastapi import FastAPI

app = FastAPI(title="Usuarios (stub)")


@app.get("/health")
def health() -> dict[str, str]:
    return {"estado": "ok", "servicio": "usuarios", "nota": "stub T2-04"}
