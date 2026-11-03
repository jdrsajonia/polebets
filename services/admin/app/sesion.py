"""
Sesión del administrador — HU2-17 (RF-21, RNF-17).

Admin no guarda usuarios ni conoce el JWT_SECRET: todo lo pregunta a Usuarios
por la red interna, igual que el Gateway.

  - Login: la CLI manda correo y contraseña a `POST /login`. Admin los reenvía
    a Usuarios (`POST /auth/login`), que responde el usuario y deja el JWT en
    la cookie `session`. Si el rol no es `admin`, Admin responde 403 y no
    entrega el token. Si lo es, devuelve el token en el cuerpo: la CLI no es un
    navegador y lo manda luego en `Authorization: Bearer <token>`.
  - Rutas protegidas: `exigir_admin` pasa ese token a Usuarios como cookie en
    `GET /internal/auth` (la misma subpetición del Gateway). El rol sale de la
    base de Usuarios, no del token: un admin degradado pierde el acceso en su
    siguiente petición.

Errores: 401 sin token o con sesión vencida, 403 con rol jugador, 503 si
Usuarios no responde y 502 si responde algo inesperado.
"""
import logging
import os
import re
from typing import Annotated, Any, Iterator

import httpx
from fastapi import Depends, Header, HTTPException
from pydantic import BaseModel, ConfigDict, Field

log = logging.getLogger("admin.sesion")

# Cookie y headers del contrato de Usuarios (services/usuarios/README.md).
COOKIE_SESION = "session"
HEADER_USUARIO = "X-User-Id"
HEADER_ROL = "X-User-Rol"
ROL_ADMIN = "admin"
TIMEOUT_USUARIOS_S = 5.0

MENSAJE_CREDENCIALES = "Correo o contraseña incorrectos"
MENSAJE_SIN_SESION = "Inicia sesión como administrador"
MENSAJE_SESION_INVALIDA = "La sesión no es válida o ya venció"
MENSAJE_SOLO_ADMIN = "Solo los administradores pueden usar la CLI"
MENSAJE_USUARIOS_CAIDO = "Usuarios no disponible"

# Un JWT solo usa base64url y puntos. Cualquier otra cosa se rechaza antes de
# armar la cookie, para que el token no pueda inyectar otros valores en ella.
_FORMATO_TOKEN = re.compile(r"^[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+){2}$")


def _url_usuarios() -> str:
    # Obligatoria, sin valor por defecto (igual que RABBITMQ_URL).
    url = os.environ.get("USUARIOS_BASE_URL", "").strip().rstrip("/")
    if not url:
        raise RuntimeError("falta USUARIOS_BASE_URL para Admin")
    return url


USUARIOS_BASE_URL = _url_usuarios()


class LoginRequest(BaseModel):
    """Cuerpo de POST /login. Mismos límites que LoginIn de Usuarios."""

    model_config = ConfigDict(extra="forbid")

    email: str = Field(min_length=3, max_length=255)
    password: str = Field(min_length=1, max_length=128)


def cliente_usuarios() -> Iterator[httpx.Client]:
    """Cliente HTTP hacia Usuarios, uno por petición. Las pruebas lo reemplazan."""
    with httpx.Client(base_url=USUARIOS_BASE_URL, timeout=TIMEOUT_USUARIOS_S) as cliente:
        yield cliente


ClienteUsuarios = Annotated[httpx.Client, Depends(cliente_usuarios)]


def _llamar(peticion, *args, **kwargs) -> httpx.Response:
    """Hace la llamada a Usuarios y traduce una caída de red en 503."""
    try:
        return peticion(*args, **kwargs)
    except httpx.RequestError as e:
        log.warning("Usuarios no responde: %s", e)
        raise HTTPException(status_code=503, detail=MENSAJE_USUARIOS_CAIDO)


def iniciar_sesion(cliente: httpx.Client, datos: LoginRequest) -> dict[str, Any]:
    """Valida las credenciales en Usuarios y devuelve {token, usuario} solo si es admin."""
    resp = _llamar(cliente.post, "/auth/login", json={"email": datos.email, "password": datos.password})
    if resp.status_code == 401:
        # Mismo mensaje genérico de Usuarios: no revela si el correo existe.
        raise HTTPException(status_code=401, detail=MENSAJE_CREDENCIALES)
    if resp.status_code != 200:
        log.warning("login: Usuarios respondió %s", resp.status_code)
        raise HTTPException(status_code=502, detail=f"Error en Usuarios ({resp.status_code})")

    usuario = resp.json()
    if usuario.get("rol") != ROL_ADMIN:
        raise HTTPException(status_code=403, detail=MENSAJE_SOLO_ADMIN)
    token = resp.cookies.get(COOKIE_SESION)
    if not token:
        log.warning("login: Usuarios no devolvió la cookie %s", COOKIE_SESION)
        raise HTTPException(status_code=502, detail="Usuarios no devolvió la sesión")
    return {"token": token, "usuario": usuario}


def _token_de(authorization: str | None) -> str:
    """Saca el token de `Authorization: Bearer <token>`; 401 si falta o no tiene forma de JWT."""
    esquema, _, token = (authorization or "").partition(" ")
    token = token.strip()
    if esquema.lower() != "bearer" or not token:
        raise HTTPException(status_code=401, detail=MENSAJE_SIN_SESION, headers={"WWW-Authenticate": "Bearer"})
    if not _FORMATO_TOKEN.match(token):
        raise HTTPException(status_code=401, detail=MENSAJE_SESION_INVALIDA, headers={"WWW-Authenticate": "Bearer"})
    return token


def exigir_admin(
    cliente: ClienteUsuarios,
    authorization: Annotated[str | None, Header()] = None,
) -> str:
    """Dependencia de las rutas de la CLI: valida el token en Usuarios y exige rol admin.

    Devuelve el id del administrador.
    """
    token = _token_de(authorization)
    resp = _llamar(cliente.get, "/internal/auth", headers={"Cookie": f"{COOKIE_SESION}={token}"})
    if resp.status_code == 401:
        raise HTTPException(status_code=401, detail=MENSAJE_SESION_INVALIDA, headers={"WWW-Authenticate": "Bearer"})
    if not resp.is_success:
        log.warning("auth: Usuarios respondió %s", resp.status_code)
        raise HTTPException(status_code=502, detail=f"Error en Usuarios ({resp.status_code})")
    if resp.headers.get(HEADER_ROL) != ROL_ADMIN:
        raise HTTPException(status_code=403, detail=MENSAJE_SOLO_ADMIN)
    return resp.headers.get(HEADER_USUARIO, "")


SoloAdmin = Depends(exigir_admin)
