"""Servicio Pagos - Prototipo 2 (ArquiSoft).

Punto de entrada de FastAPI. Aqui solo se arma la aplicacion: conexion a
PostgreSQL Pagos y registro de routers. Pagos no recibe trafico del navegador:
lo llaman el Orquestador y el Motor por las rutas /internal/* (ver
app/routers/__init__.py).

Estructura:
    app/
    ├── main.py              # crea la app (este archivo)
    ├── core/config.py       # variables de entorno
    ├── core/internal_auth.py # token de servicio
    ├── db.py                # engine, sesiones, espera e inicializacion de la BD
    ├── deps.py              # dependencias: DbSession, ServicioInterno
    ├── models.py            # tablas (ver documentation/modelo-datos-p2.pdf)
    ├── schemas/             # modelos Pydantic de entrada/salida
    ├── routers/             # endpoints REST, uno por tema
    └── services/            # logica de negocio
"""
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.core.config import settings
from app.db import init_db, wait_for_db
from app.routers import ROUTERS
from app.schemas.common import MessageOut

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger("pagos")


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Al arrancar: esperar a PostgreSQL y crear las tablas que falten.
    wait_for_db()
    init_db()
    logger.info("Pagos listo.")
    yield


def create_app() -> FastAPI:
    app = FastAPI(title=settings.app_name, version=settings.app_version, lifespan=lifespan)

    for router in ROUTERS:
        app.include_router(router)

    @app.get("/", response_model=MessageOut, tags=["health"])
    def root() -> MessageOut:
        return MessageOut(service="pagos", message="Pagos - Prototipo 2")

    return app


app = create_app()
