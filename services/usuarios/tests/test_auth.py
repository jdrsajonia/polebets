"""HU2-02: login, logout, /auth/me y la validacion de sesion para el Gateway."""
import uuid
from datetime import datetime, timedelta, timezone

import jwt
from sqlalchemy import delete

from app.config import settings
from app.core.security import create_access_token, decode_access_token
from app.db import SessionLocal
from app.models import User
from tests.conftest import requires_db

COOKIE = settings.session_cookie_name


def _login(client, email, password):
    return client.post("/auth/login", json={"email": email, "password": password})


# ---------------------------------------------------------------------------
# Login
# ---------------------------------------------------------------------------
@requires_db
def test_login_correcto_pone_cookie_httponly_con_el_jwt(client, jugador):
    r = _login(client, "  " + jugador["email"].upper() + " ", jugador["contrasena"])

    assert r.status_code == 200
    assert r.json() == {"id": jugador["id"], "nombre": "Ana Pérez", "email": jugador["email"], "rol": "player"}
    assert "token" not in r.text  # el JWT nunca va en el cuerpo
    cookie = r.headers["set-cookie"]
    assert cookie.startswith(f"{COOKIE}=")
    assert "HttpOnly" in cookie and "samesite=lax" in cookie.lower() and "Path=/" in cookie
    assert r.headers["cache-control"] == "no-store"

    user_id, rol = decode_access_token(r.cookies[COOKIE])
    assert str(user_id) == jugador["id"] and rol == "player"


@requires_db
def test_credenciales_invalidas_responden_el_mismo_mensaje(client, jugador):
    clave_mala = _login(client, jugador["email"], "otra-clave")
    correo_desconocido = _login(client, "nadie@arqui.local", jugador["contrasena"])

    for r in (clave_mala, correo_desconocido):
        assert r.status_code == 401
        assert r.json() == {"detail": "Correo o contraseña incorrectos"}
        assert "set-cookie" not in r.headers


@requires_db
def test_el_admin_inicial_existe_y_puede_iniciar_sesion(client):
    r = _login(client, settings.admin_email, settings.admin_password)
    assert r.status_code == 200
    assert r.json()["rol"] == "admin"


# ---------------------------------------------------------------------------
# /auth/me y logout
# ---------------------------------------------------------------------------
@requires_db
def test_me_con_y_sin_sesion(client, jugador):
    assert client.get("/auth/me").status_code == 401

    _login(client, jugador["email"], jugador["contrasena"])  # el TestClient guarda la cookie
    r = client.get("/auth/me")
    assert r.status_code == 200
    assert r.json()["email"] == jugador["email"]


@requires_db
def test_logout_borra_la_cookie_y_la_siguiente_peticion_es_401(client, jugador):
    _login(client, jugador["email"], jugador["contrasena"])
    assert client.get("/internal/auth").status_code == 204

    r = client.post("/auth/logout")
    assert r.status_code == 204
    assert f'{COOKIE}=""' in r.headers["set-cookie"] and "Max-Age=0" in r.headers["set-cookie"]
    assert client.get("/internal/auth").status_code == 401
    assert client.get("/auth/me").status_code == 401


# ---------------------------------------------------------------------------
# GET /internal/auth (auth_request del Gateway)
# ---------------------------------------------------------------------------
@requires_db
def test_sesion_valida_devuelve_los_headers_del_gateway(client, jugador):
    _login(client, jugador["email"], jugador["contrasena"])
    r = client.get("/internal/auth")

    assert r.status_code == 204
    assert r.headers["x-user-id"] == jugador["id"]
    assert r.headers["x-user-rol"] == "player"


@requires_db
def test_sin_cookie_o_token_alterado_es_401(client, jugador):
    assert client.get("/internal/auth").status_code == 401

    bueno = create_access_token(jugador["id"], "player")
    alterado = bueno[:-2] + ("aa" if not bueno.endswith("aa") else "bb")
    otra_clave = jwt.encode({"sub": jugador["id"], "rol": "player",
                             "exp": datetime.now(timezone.utc) + timedelta(hours=1)},
                            "otro-secreto", algorithm="HS256")
    vencido = jwt.encode({"sub": jugador["id"], "rol": "player",
                          "exp": datetime.now(timezone.utc) - timedelta(minutes=1)},
                         settings.jwt_secret, algorithm="HS256")

    for token in (alterado, otra_clave, vencido, "basura"):
        client.cookies.set(COOKIE, token)
        assert client.get("/internal/auth").status_code == 401, token


@requires_db
def test_el_rol_sale_de_la_bd_y_un_usuario_borrado_pierde_la_sesion(client, jugador):
    # Un token que dice "admin" no convierte en admin a un jugador.
    client.cookies.set(COOKIE, create_access_token(jugador["id"], "admin"))
    assert client.get("/internal/auth").headers["x-user-rol"] == "player"

    with SessionLocal() as db:
        db.execute(delete(User).where(User.id == uuid.UUID(jugador["id"])))
        db.commit()
    assert client.get("/internal/auth").status_code == 401


@requires_db
def test_internal_auth_no_pide_token_de_servicio_pero_users_si(client):
    # /internal/auth la llama Nginx, que no tiene el token; /internal/users si lo exige.
    assert client.get("/internal/auth").status_code == 401  # 401 por sesion, no por token
    assert client.get(f"/internal/users/{uuid.uuid4()}").status_code == 401
