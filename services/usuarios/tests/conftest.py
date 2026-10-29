"""Configuracion de pruebas (ver services/pagos/tests/conftest.py para levantar un PostgreSQL).

    DATABASE_URL=postgresql+psycopg://arqui:arqui_pass@localhost:55432/usuarios_db pytest

Si PostgreSQL no esta disponible, esas pruebas se saltan (skip).
"""
import os

import pytest

os.environ.setdefault("DATABASE_URL", "postgresql+psycopg://arqui:arqui_pass@localhost:5432/usuarios_db")
os.environ.setdefault("DB_CONNECT_RETRIES", "1")
os.environ.setdefault("DB_CONNECT_RETRY_SECONDS", "0")

from app.db import ping_db  # noqa: E402

requires_db = pytest.mark.skipif(not ping_db(), reason="PostgreSQL no disponible")


@pytest.fixture
def client():
    from fastapi.testclient import TestClient

    from app.main import app

    with TestClient(app) as c:  # "with" ejecuta el lifespan (crea tablas)
        yield c
