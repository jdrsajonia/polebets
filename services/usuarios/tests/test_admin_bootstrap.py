"""Administrador inicial (RF-21): se crea una vez y nunca pisa una cuenta de jugador."""
import uuid

import pytest
from sqlalchemy import delete

from app.core.security import verify_password
from app.db import SessionLocal
from app.models import ROLE_ADMIN, User
from app.services.admin_bootstrap import AdminIncompatible, aprovisionar_admin
from app.services.auth import buscar_por_email
from tests.conftest import requires_db


@pytest.fixture
def correo(client):
    email = f"admin-{uuid.uuid4().hex[:8]}@arqui.local"
    yield email
    with SessionLocal() as db:
        db.execute(delete(User).where(User.email == email))
        db.commit()


@requires_db
def test_crea_el_admin_una_sola_vez(correo):
    with SessionLocal() as db:
        assert aprovisionar_admin(db, email=correo.upper(), nombre="Admin", contrasena="clave-admin") is True
        assert aprovisionar_admin(db, email=correo, nombre="Admin", contrasena="otra") is False
        admin = buscar_por_email(db, correo)
        assert admin.role == ROLE_ADMIN
        assert verify_password("clave-admin", admin.password_hash)  # la segunda vez no cambio la clave


@requires_db
def test_no_convierte_en_admin_a_un_jugador(correo):
    with SessionLocal() as db:
        db.add(User(email=correo, name="Jugador", password_hash="x"))
        db.commit()
        with pytest.raises(AdminIncompatible):
            aprovisionar_admin(db, email=correo, nombre="Admin", contrasena="clave-admin")
        assert buscar_por_email(db, correo).role == "player"
