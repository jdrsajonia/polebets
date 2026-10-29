"""Tabla users en PostgreSQL Usuarios (T2-03)."""
import uuid

import pytest
from sqlalchemy import delete, inspect
from sqlalchemy.exc import IntegrityError

from app.db import SessionLocal, engine
from app.models import ROLE_PLAYER, User
from tests.conftest import requires_db


def _usuario(**cambios):
    datos = dict(id=uuid.uuid4(), email=f"test-{uuid.uuid4().hex[:8]}@arqui.local", name="Prueba", password_hash="hash")
    datos.update(cambios)
    return User(**datos)


@pytest.fixture
def borrar():
    ids = []
    yield ids
    with SessionLocal() as db:
        db.execute(delete(User).where(User.id.in_(ids)))
        db.commit()


@requires_db
def test_health_y_tabla_users_sin_saldo(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"service": "usuarios", "status": "ok", "postgres": True}

    columnas = {c["name"] for c in inspect(engine).get_columns("users")}
    assert columnas == {"id", "email", "name", "password_hash", "role", "created_at"}


@requires_db
def test_guarda_el_id_que_le_dan_y_por_defecto_es_jugador(client, borrar):
    usuario = _usuario()
    borrar.append(usuario.id)
    with SessionLocal() as db:
        db.add(usuario)
        db.commit()
        guardado = db.get(User, usuario.id)
        assert guardado.role == ROLE_PLAYER
        assert guardado.created_at is not None


@requires_db
def test_correo_unico_y_rol_valido(client, borrar):
    primero = _usuario()
    borrar.append(primero.id)
    with SessionLocal() as db:
        db.add(primero)
        db.commit()

    for invalido in (_usuario(email=primero.email), _usuario(role="superadmin")):
        with SessionLocal() as db:
            db.add(invalido)
            with pytest.raises(IntegrityError):
                db.commit()
