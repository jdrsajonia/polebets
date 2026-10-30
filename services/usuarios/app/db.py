"""Conexion a PostgreSQL Usuarios con SQLAlchemy (mismo patron que services/pagos)."""
import logging
import time
from collections.abc import Iterator

from sqlalchemy import create_engine, text
from sqlalchemy.exc import OperationalError
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

from app.config import settings

logger = logging.getLogger(__name__)

engine = create_engine(settings.database_url, pool_pre_ping=True, hide_parameters=True)
SessionLocal = sessionmaker(bind=engine, autoflush=False, autocommit=False, expire_on_commit=False)


class Base(DeclarativeBase):
    """Base declarativa para los modelos ORM."""


def get_db() -> Iterator[Session]:
    """Dependencia de FastAPI que entrega una sesion de BD por request."""
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()


def ping_db() -> bool:
    """True si PostgreSQL responde a un SELECT 1."""
    try:
        with engine.connect() as conn:
            conn.execute(text("SELECT 1"))
        return True
    except OperationalError:
        return False


def wait_for_db(retries: int | None = None, delay: float | None = None) -> None:
    """Espera a que PostgreSQL acepte conexiones."""
    retries = settings.db_connect_retries if retries is None else retries
    delay = settings.db_connect_retry_seconds if delay is None else delay
    for attempt in range(1, retries + 1):
        if ping_db():
            return
        logger.warning("PostgreSQL no responde (intento %d/%d), reintentando...", attempt, retries)
        time.sleep(delay)
    raise RuntimeError("No fue posible conectarse a PostgreSQL")


def init_db() -> None:
    """Crea las tablas que falten (create_all no modifica tablas existentes)."""
    from app import models  # noqa: F401  (registra los modelos en Base.metadata)

    Base.metadata.create_all(bind=engine)
