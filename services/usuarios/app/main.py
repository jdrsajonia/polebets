"""Usuarios — base del servicio (T2-04, T2-03).

Hoy trae la base de datos (tabla users en PostgreSQL Usuarios, T2-03) y
/health. Las rutas son de HU2-01/HU2-02/HU2-03 (Misael):
  - Cuentas, contraseñas (bcrypt/Argon2), roles y emisión del JWT (HU2-02).
  - POST /auth/login, POST /auth/logout.
  - GET /internal/auth (subpetición auth_request del Gateway, T2-01).
  - POST /internal/users, GET /internal/users/{id} (contrato del Orquestador).

Puerto interno: 8001 (ver documentation/orquestador-api.md, USUARIOS_BASE_URL).
"""
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Response, status

from app.db import init_db, ping_db, wait_for_db

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger("usuarios")


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Al arrancar: esperar a PostgreSQL y crear las tablas que falten.
    wait_for_db()
    init_db()
    logger.info("Usuarios listo.")
    yield


app = FastAPI(title="Usuarios", lifespan=lifespan)


@app.get("/health")
def health(response: Response) -> dict[str, str | bool]:
    """200 si el servicio y PostgreSQL responden; 503 si la BD no responde."""
    db_ok = ping_db()
    if not db_ok:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
    return {"service": "usuarios", "status": "ok" if db_ok else "degraded", "postgres": db_ok}
