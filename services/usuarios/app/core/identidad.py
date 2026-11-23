"""Identidad firmada para los servicios internos (escenario 4, T3-05).

En cada GET /internal/auth (auth_request del Gateway), Usuarios firma un token
corto que dice quien hace la peticion. El Gateway lo reenvia en X-User-Token y
el Orquestador y el Motor lo verifican con la llave PUBLICA. Asi una cabecera
escrita a mano ya no sirve: sin la llave privada, que solo tiene Usuarios, no
hay firma valida.

No es la cookie de sesion (security.py): la cookie dura horas, va al navegador
y solo la valida Usuarios (HS256). Este token dura segundos y nunca sale de la
red interna.

Contrato (lo verifican services/orquestador/internal/sesion y
services/motor/internal/auth; si cambia aqui, cambia alla):

  - Algoritmo EdDSA (Ed25519). Llaves de scripts/generar-llaves-identidad.sh.
  - Claims: "sub" = id del usuario, "rol" = "player" | "admin", "iss" =
    "usuarios", "aud" = "polebet", "iat" y "exp" (iat + IDENTIDAD_TTL_SEGUNDOS).
"""
import uuid
from datetime import datetime, timedelta, timezone
from functools import lru_cache
from pathlib import Path

import jwt
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import load_pem_private_key

from app.config import settings
from app.core.security import CLAIM_ROLE, CLAIM_USER_ID, VALID_ROLES

# Header en el que viaja el token (Usuarios -> Gateway -> Orquestador/Motor).
HEADER_IDENTIDAD = "X-User-Token"

ALGORITMO = "EdDSA"
EMISOR = "usuarios"
AUDIENCIA = "polebet"


class LlaveInvalida(Exception):
    """El archivo de la llave privada no existe o no es una llave Ed25519."""


@lru_cache
def llave_privada() -> Ed25519PrivateKey:
    """Lee la llave privada una sola vez (se llama al arrancar para fallar pronto)."""
    ruta = Path(settings.identidad_llave_privada)
    try:
        llave = load_pem_private_key(ruta.read_bytes(), password=None)
    except (OSError, ValueError, TypeError) as exc:
        raise LlaveInvalida(f"no se pudo leer la llave privada de identidad en {ruta}: {exc}") from exc
    if not isinstance(llave, Ed25519PrivateKey):
        raise LlaveInvalida(f"la llave de {ruta} no es Ed25519 (generarla con scripts/generar-llaves-identidad.sh)")
    return llave


def firmar_identidad(user_id: uuid.UUID | str, rol: str) -> str:
    """Token de identidad de corta duracion para el usuario y rol dados."""
    if rol not in VALID_ROLES:
        raise ValueError(f"rol desconocido: {rol!r}")
    ahora = datetime.now(timezone.utc)
    claims = {
        CLAIM_USER_ID: str(user_id),
        CLAIM_ROLE: rol,
        "iss": EMISOR,
        "aud": AUDIENCIA,
        "iat": ahora,
        "exp": ahora + timedelta(seconds=settings.identidad_ttl_segundos),
    }
    return jwt.encode(claims, llave_privada(), algorithm=ALGORITMO)
