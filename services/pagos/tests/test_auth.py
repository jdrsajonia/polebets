"""Tests de HU-01: registro de usuario."""
import uuid

import pytest
from sqlalchemy import select

from app.core.security import verify_password
from app.models import ROLE_PLAYER, User


def _payload(**overrides):
    base = {
        "name": "Ivan Test",
        "email": f"ivan-{uuid.uuid4().hex[:8]}@example.com",
        "password": "clave-segura-123",
    }
    base.update(overrides)
    return base


# ---------------------------------------------------------------------------
# Caso feliz
# ---------------------------------------------------------------------------
def test_registro_exitoso(client):
    """HU-01: crea usuario con rol player, saldo 0 y password hasheado."""
    from app.db import SessionLocal

    payload = _payload()
    response = client.post("/auth/register", json=payload)

    assert response.status_code == 201
    body = response.json()
    assert body["email"] == payload["email"]
    assert body["name"] == payload["name"]
    assert body["role"] == ROLE_PLAYER
    assert body["balance"] == 0
    assert "id" in body
    assert "password" not in body
    assert "password_hash" not in body

    with SessionLocal() as db:
        user = db.scalar(select(User).where(User.email == payload["email"]))
        assert user is not None
        assert user.password_hash != payload["password"]
        assert verify_password(payload["password"], user.password_hash)
        assert user.balance == 0
        assert user.role == ROLE_PLAYER

        db.delete(user)
        db.commit()


# ---------------------------------------------------------------------------
# Correo repetido
# ---------------------------------------------------------------------------
def test_correo_repetido_devuelve_409(client):
    """HU-01: correo repetido -> 409 y mensaje exacto."""
    from app.db import SessionLocal

    payload = _payload()
    r1 = client.post("/auth/register", json=payload)
    assert r1.status_code == 201

    r2 = client.post("/auth/register", json=payload)
    assert r2.status_code == 409
    assert r2.json()["detail"] == "Este correo ya está registrado"

    with SessionLocal() as db:
        user = db.scalar(select(User).where(User.email == payload["email"]))
        if user:
            db.delete(user)
            db.commit()


# ---------------------------------------------------------------------------
# Datos invalidos
# ---------------------------------------------------------------------------
@pytest.mark.parametrize(
    "override, motivo",
    [
        ({"email": "no-es-correo"}, "correo invalido"),
        ({"email": ""}, "correo vacio"),
        ({"name": ""}, "nombre vacio"),
        ({"password": "corta"}, "password muy corta"),
        ({"password": ""}, "password vacia"),
    ],
)
def test_datos_invalidos_devuelven_422(client, override, motivo):
    """HU-01: campo vacio o correo invalido -> 422."""
    response = client.post("/auth/register", json=_payload(**override))
    assert response.status_code == 422, f"fallo: {motivo}"