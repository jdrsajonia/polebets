"""HU2-01 (paso 2 del registro orquestado) y HU2-03 (perfil): /internal/users."""
import uuid

import pytest

from app.core.security import verify_password
from app.db import SessionLocal
from app.models import User
from tests.conftest import SERVICIO, requires_db


def _datos(**cambios):
    datos = {
        "id": str(uuid.uuid4()),
        "nombre": "  Juan Gómez ",
        "email": f"Juan-{uuid.uuid4().hex[:8]}@Correo.Co",
        "contrasena": "clave-segura-123",
    }
    datos.update(cambios)
    return datos


@requires_db
def test_crea_jugador_con_el_id_del_orquestador(client, borrar):
    datos = _datos()
    borrar.append(uuid.UUID(datos["id"]))
    r = client.post("/internal/users", json=datos, headers={**SERVICIO, "Idempotency-Key": f"user:{datos['id']}"})

    assert r.status_code == 201
    assert r.json() == {"id": datos["id"], "nombre": "Juan Gómez", "email": datos["email"].lower(), "rol": "player"}
    with SessionLocal() as db:
        guardado = db.get(User, uuid.UUID(datos["id"]))
        assert guardado.password_hash != datos["contrasena"]  # nunca en texto plano
        assert verify_password(datos["contrasena"], guardado.password_hash)


@requires_db
def test_reintento_con_el_mismo_id_responde_200_sin_duplicar(client, borrar):
    datos = _datos()
    borrar.append(uuid.UUID(datos["id"]))
    primero = client.post("/internal/users", json=datos, headers=SERVICIO)
    segundo = client.post("/internal/users", json=datos, headers=SERVICIO)

    assert (primero.status_code, segundo.status_code) == (201, 200)
    assert primero.json() == segundo.json()


@requires_db
def test_correo_ya_registrado_es_409_aunque_cambien_mayusculas(client, borrar):
    datos = _datos()
    borrar.append(uuid.UUID(datos["id"]))
    client.post("/internal/users", json=datos, headers=SERVICIO)

    otro = _datos(email=datos["email"].upper())
    r = client.post("/internal/users", json=otro, headers=SERVICIO)
    assert r.status_code == 409
    assert r.json() == {"detail": "Este correo ya está registrado"}
    with SessionLocal() as db:
        assert db.get(User, uuid.UUID(otro["id"])) is None  # no se creo nada


@requires_db
@pytest.mark.parametrize(
    "cambio",
    [{"email": "no-es-correo"}, {"contrasena": "corta"}, {"nombre": "   "}, {"rol": "admin"}, {"id": "no-uuid"}],
)
def test_datos_invalidos_son_422(client, cambio):
    r = client.post("/internal/users", json=_datos(**cambio), headers=SERVICIO)
    assert r.status_code == 422  # el Orquestador lo traduce a cuerpo_invalido


@requires_db
def test_exige_token_de_servicio_y_clave_correcta(client):
    datos = _datos()
    assert client.post("/internal/users", json=datos).status_code == 401
    assert client.post("/internal/users", json=datos, headers={"X-Internal-Service-Token": "x" * 40}).status_code == 401
    otra_clave = {**SERVICIO, "Idempotency-Key": f"user:{uuid.uuid4()}"}
    assert client.post("/internal/users", json=datos, headers=otra_clave).status_code == 400


@requires_db
def test_perfil_por_id(client, jugador):
    r = client.get(f"/internal/users/{jugador['id']}", headers=SERVICIO)
    assert r.status_code == 200
    assert r.json() == {"id": jugador["id"], "nombre": "Ana Pérez", "email": jugador["email"], "rol": "player"}
    assert client.get(f"/internal/users/{uuid.uuid4()}", headers=SERVICIO).status_code == 404
