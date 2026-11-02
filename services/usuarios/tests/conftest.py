"""Configuracion de pruebas (ver services/pagos/tests/conftest.py para levantar un PostgreSQL).

    DATABASE_URL=postgresql+psycopg://arqui:arqui_pass@localhost:55432/usuarios_db pytest

Si PostgreSQL no esta disponible, esas pruebas se saltan (skip).
"""
import os
import uuid

import pytest

os.environ.setdefault("DATABASE_URL", "postgresql+psycopg://arqui:arqui_pass@localhost:5432/usuarios_db")
os.environ.setdefault("DB_CONNECT_RETRIES", "1")
os.environ.setdefault("DB_CONNECT_RETRY_SECONDS", "0")
os.environ.setdefault("JWT_SECRET", "secreto-de-pruebas-que-no-se-usa-en-ningun-lado")
os.environ.setdefault("INTERNAL_API_TOKEN", "token-de-servicio-de-pruebas-32-caracteres")
os.environ.setdefault("ADMIN_EMAIL", "admin-pruebas@correo.co")
os.environ.setdefault("ADMIN_PASSWORD", "clave-admin-pruebas")

from app.db import ping_db  # noqa: E402

requires_db = pytest.mark.skipif(not ping_db(), reason="PostgreSQL no disponible")

TOKEN_SERVICIO = os.environ["INTERNAL_API_TOKEN"]
SERVICIO = {"X-Internal-Service-Token": TOKEN_SERVICIO}


@pytest.fixture
def client():
    from fastapi.testclient import TestClient

    from app.main import app

    with TestClient(app) as c:  # "with" ejecuta el lifespan (crea tablas y el admin)
        yield c


@pytest.fixture
def borrar():
    """Ids de usuarios creados en la prueba; se borran al terminar."""
    from sqlalchemy import delete

    from app.db import SessionLocal
    from app.models import User

    ids: list[uuid.UUID] = []
    yield ids
    with SessionLocal() as db:
        db.execute(delete(User).where(User.id.in_(ids)))
        db.commit()


@pytest.fixture
def jugador(client, borrar):
    """Crea un jugador por la ruta interna del registro y devuelve sus datos."""
    datos = {
        "id": str(uuid.uuid4()),
        "nombre": "Ana Pérez",
        "email": f"ana-{uuid.uuid4().hex[:8]}@correo.co",
        "contrasena": "clave-segura-123",
    }
    r = client.post("/internal/users", json=datos, headers=SERVICIO)
    assert r.status_code == 201, r.text
    borrar.append(uuid.UUID(datos["id"]))
    return datos
