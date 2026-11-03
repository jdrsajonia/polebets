"""Configuración de pruebas de Admin. No necesitan el stack levantado:

    cd services/admin
    pip install -r requirements-dev.txt
    pytest

Usuarios se reemplaza por un falso (httpx.MockTransport) y el Broker por un
`rpc_call` falso, así se prueba solo la lógica de Admin.
"""
import json
import os

import httpx
import pytest

# Admin exige estas variables al importarse; aquí no se conecta a nada.
os.environ.setdefault("RABBITMQ_URL", "amqp://pruebas:pruebas@broker-falso:5672/")
os.environ.setdefault("USUARIOS_BASE_URL", "http://usuarios-falso:8001")

from app import main  # noqa: E402
from app.sesion import cliente_usuarios  # noqa: E402

# Tokens con forma de JWT (tres partes base64url), como los que emite Usuarios.
TOKEN_ADMIN = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.firma-admin"
TOKEN_JUGADOR = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJqdWdhZG9yIn0.firma-jugador"
ID_ADMIN = "11111111-1111-1111-1111-111111111111"
ID_JUGADOR = "22222222-2222-2222-2222-222222222222"

# Cuentas que conoce el Usuarios falso: correo -> (contraseña, usuario, token).
CUENTAS = {
    "admin@arqui.local": ("clave-admin", {"id": ID_ADMIN, "nombre": "Administrador", "email": "admin@arqui.local", "rol": "admin"}, TOKEN_ADMIN),
    "ana@arqui.local": ("clave-ana", {"id": ID_JUGADOR, "nombre": "Ana", "email": "ana@arqui.local", "rol": "player"}, TOKEN_JUGADOR),
}
# Sesiones válidas para GET /internal/auth: token -> (id, rol).
SESIONES = {TOKEN_ADMIN: (ID_ADMIN, "admin"), TOKEN_JUGADOR: (ID_JUGADOR, "player")}


class UsuariosFalso:
    """Imita POST /auth/login y GET /internal/auth de Usuarios y anota cada llamada."""

    def __init__(self):
        self.caido = False
        self.llamadas: list[str] = []

    def manejar(self, peticion: httpx.Request) -> httpx.Response:
        """Responde como Usuarios según la ruta; si está caído, falla la conexión."""
        self.llamadas.append(peticion.url.path)
        if self.caido:
            raise httpx.ConnectError("Usuarios apagado", request=peticion)
        if peticion.method == "POST" and peticion.url.path == "/auth/login":
            return self._login(json.loads(peticion.content))
        if peticion.method == "GET" and peticion.url.path == "/internal/auth":
            return self._validar(peticion.headers.get("cookie", ""))
        return httpx.Response(404)

    def _login(self, datos: dict) -> httpx.Response:
        """200 con el usuario y la cookie de sesión, o 401 con el mensaje genérico."""
        cuenta = CUENTAS.get(datos.get("email"))
        if cuenta is None or cuenta[0] != datos.get("password"):
            return httpx.Response(401, json={"detail": "Correo o contraseña incorrectos"})
        _, usuario, token = cuenta
        return httpx.Response(200, json=usuario, headers={"set-cookie": f"session={token}; HttpOnly; Path=/; SameSite=lax"})

    def _validar(self, cookie: str) -> httpx.Response:
        """204 con X-User-Id y X-User-Rol si la cookie es una sesión conocida; 401 si no."""
        token = cookie.removeprefix("session=")
        if token not in SESIONES:
            return httpx.Response(401, json={"detail": "No hay sesión"})
        user_id, rol = SESIONES[token]
        return httpx.Response(204, headers={"X-User-Id": user_id, "X-User-Rol": rol})


@pytest.fixture
def usuarios():
    """El Usuarios falso que atiende las llamadas de Admin en cada prueba."""
    falso = UsuariosFalso()
    transporte = httpx.MockTransport(falso.manejar)
    main.app.dependency_overrides[cliente_usuarios] = lambda: httpx.Client(base_url="http://usuarios-falso:8001", transport=transporte)
    yield falso
    main.app.dependency_overrides.pop(cliente_usuarios, None)


@pytest.fixture
def broker(monkeypatch):
    """Reemplaza rpc_call: guarda los mensajes publicados y responde lo que diga `respuesta`."""

    class BrokerFalso:
        def __init__(self):
            self.publicados: list[tuple[str, dict]] = []
            self.respuesta: dict = {"ok": True, "carreras": []}

        def rpc_call(self, cola, mensaje, timeout=None):
            self.publicados.append((cola, mensaje))
            return self.respuesta

    falso = BrokerFalso()
    monkeypatch.setattr(main, "rpc_call", falso.rpc_call)
    return falso


@pytest.fixture
def client(usuarios, broker):
    """Cliente de Admin con Usuarios y el Broker falsos ya conectados."""
    from fastapi.testclient import TestClient

    with TestClient(main.app) as c:
        yield c


def bearer(token: str) -> dict[str, str]:
    """Header Authorization que manda la CLI."""
    return {"Authorization": f"Bearer {token}"}
