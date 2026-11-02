"""Usuarios — cuentas, contraseñas, roles y sesion (HU2-01, HU2-02, HU2-03).

Dueño de PostgreSQL Usuarios (tabla users). Es el unico que emite el JWT de
sesion y el que valida la cookie para el Gateway en cada ruta protegida.

    POST /auth/login, POST /auth/logout, GET /auth/me   (via Gateway, /api/auth/*)
    GET  /internal/auth                                 (auth_request del Gateway)
    POST /internal/users, GET /internal/users/{id}      (Orquestador, token de servicio)
    GET  /health

Puerto interno: 8001 (USUARIOS_BASE_URL en el contrato). No se publica al host.
"""
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Response, status

from app.db import SessionLocal, init_db, ping_db, wait_for_db
from app.routers import ROUTERS
from app.services.admin_bootstrap import aprovisionar_admin_desde_config

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger("usuarios")


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Al arrancar: esperar a PostgreSQL, crear las tablas que falten y el admin.
    wait_for_db()
    init_db()
    with SessionLocal() as db:
        aprovisionar_admin_desde_config(db)
    logger.info("Usuarios listo.")
    yield


app = FastAPI(title="Usuarios", lifespan=lifespan)

for router in ROUTERS:
    app.include_router(router)


@app.get("/health")
def health(response: Response) -> dict[str, str | bool]:
    """200 si el servicio y PostgreSQL responden; 503 si la BD no responde."""
    db_ok = ping_db()
    if not db_ok:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
    return {"service": "usuarios", "status": "ok" if db_ok else "degraded", "postgres": db_ok}
