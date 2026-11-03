"""HU2-17 (RF-21, RNF-17): login de la CLI y rutas que exigen rol admin."""
import pytest

from tests.conftest import ID_ADMIN, TOKEN_ADMIN, TOKEN_JUGADOR, bearer

# Rutas que usa la CLI, con un cuerpo válido para cada una.
RUTAS_PROTEGIDAS = [
    ("GET", "/carreras", None),
    ("POST", "/simulacion", {"race_id": "9165", "minutos_anticipacion": 10}),
    ("POST", "/consultas", {"query": "rentabilidad", "params": {}}),
]


# --- POST /login ---
def test_login_de_admin_devuelve_el_token_y_el_usuario(client):
    r = client.post("/login", json={"email": "admin@arqui.local", "password": "clave-admin"})
    assert r.status_code == 200
    cuerpo = r.json()
    assert cuerpo["token"] == TOKEN_ADMIN
    assert cuerpo["usuario"] == {"id": ID_ADMIN, "nombre": "Administrador", "email": "admin@arqui.local", "rol": "admin"}


def test_login_de_jugador_se_rechaza_sin_entregar_el_token(client):
    r = client.post("/login", json={"email": "ana@arqui.local", "password": "clave-ana"})
    assert r.status_code == 403
    assert r.json() == {"detail": "Solo los administradores pueden usar la CLI"}
    assert TOKEN_JUGADOR not in r.text


def test_login_con_clave_mala_da_el_mensaje_generico(client):
    r = client.post("/login", json={"email": "admin@arqui.local", "password": "otra"})
    assert r.status_code == 401
    assert r.json() == {"detail": "Correo o contraseña incorrectos"}


def test_login_con_cuerpo_incompleto_no_llama_a_usuarios(client, usuarios):
    r = client.post("/login", json={"email": "admin@arqui.local"})
    assert r.status_code == 422
    assert usuarios.llamadas == []


def test_login_con_usuarios_caido_responde_503(client, usuarios):
    usuarios.caido = True
    r = client.post("/login", json={"email": "admin@arqui.local", "password": "clave-admin"})
    assert r.status_code == 503
    assert r.json() == {"detail": "Usuarios no disponible"}


# --- Rutas protegidas ---
def test_health_sigue_siendo_publico(client):
    assert client.get("/health").status_code == 200


@pytest.mark.parametrize("metodo,ruta,cuerpo", RUTAS_PROTEGIDAS)
def test_sin_token_responde_401_y_no_publica_nada(client, broker, metodo, ruta, cuerpo):
    r = client.request(metodo, ruta, json=cuerpo)
    assert r.status_code == 401
    assert r.headers["www-authenticate"] == "Bearer"
    assert broker.publicados == []


@pytest.mark.parametrize("metodo,ruta,cuerpo", RUTAS_PROTEGIDAS)
def test_con_token_de_jugador_responde_403_y_no_publica_nada(client, broker, metodo, ruta, cuerpo):
    r = client.request(metodo, ruta, json=cuerpo, headers=bearer(TOKEN_JUGADOR))
    assert r.status_code == 403
    assert broker.publicados == []


@pytest.mark.parametrize("metodo,ruta,cuerpo", RUTAS_PROTEGIDAS)
def test_con_token_de_admin_llega_al_broker(client, broker, metodo, ruta, cuerpo):
    broker.respuesta = {"ok": True, "carreras": [], "simulacion_id": "s1", "hora_largada": "2026-11-03T20:00:00Z", "tipo": "rentabilidad", "datos": {}}
    r = client.request(metodo, ruta, json=cuerpo, headers=bearer(TOKEN_ADMIN))
    assert r.status_code == 200
    assert len(broker.publicados) == 1


def test_token_vencido_o_desconocido_responde_401(client):
    r = client.get("/carreras", headers=bearer("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.vencido"))
    assert r.status_code == 401
    assert r.json() == {"detail": "La sesión no es válida o ya venció"}


@pytest.mark.parametrize("valor", [
    "Basic YWRtaW46Y2xhdmU=",              # otro esquema
    "Bearer",                              # sin token
    "Bearer no-es-un-jwt",                 # sin las tres partes
    f"Bearer {TOKEN_JUGADOR}; session={TOKEN_ADMIN}",  # intento de meter otra cookie
])
def test_authorization_mal_formado_se_rechaza_sin_llamar_a_usuarios(client, usuarios, valor):
    r = client.get("/carreras", headers={"Authorization": valor})
    assert r.status_code == 401
    assert usuarios.llamadas == []


def test_validar_el_token_con_usuarios_caido_responde_503(client, usuarios, broker):
    usuarios.caido = True
    r = client.get("/carreras", headers=bearer(TOKEN_ADMIN))
    assert r.status_code == 503
    assert broker.publicados == []


def test_el_token_viaja_a_usuarios_como_cookie_de_sesion(client, usuarios):
    # Admin no valida el JWT: se lo pregunta a Usuarios, igual que el Gateway.
    client.get("/carreras", headers=bearer(TOKEN_ADMIN))
    assert usuarios.llamadas == ["/internal/auth"]
