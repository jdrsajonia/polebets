"""Escenario 4 (T3-05): firma de la identidad que verifican el Orquestador y el Motor.

No necesitan PostgreSQL: prueban solo app/core/identidad.py.
"""
import uuid

import jwt
import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from app.core.identidad import firmar_identidad
from tests.conftest import LLAVE_PUBLICA_PRUEBA

USUARIO = uuid.uuid4()


def _decodificar(token, llave=LLAVE_PUBLICA_PRUEBA):
    """Verifica como lo hacen el Orquestador y el Motor: algoritmo, emisor y audiencia fijos."""
    return jwt.decode(
        token,
        llave,
        algorithms=["EdDSA"],
        audience="polebet",
        issuer="usuarios",
        options={"require": ["exp", "iat", "sub", "rol", "iss", "aud"]},
    )


def test_el_token_lleva_id_rol_emisor_audiencia_y_vence_en_60_segundos():
    claims = _decodificar(firmar_identidad(USUARIO, "player"))

    assert claims["sub"] == str(USUARIO)
    assert claims["rol"] == "player"
    assert claims["iss"] == "usuarios"
    assert claims["aud"] == "polebet"
    assert claims["exp"] - claims["iat"] == 60


def test_va_firmado_con_eddsa():
    assert jwt.get_unverified_header(firmar_identidad(USUARIO, "admin"))["alg"] == "EdDSA"


def test_otra_llave_publica_no_lo_verifica():
    # Una llave que no es la de Usuarios no sirve: la firma no coincide.
    otra = Ed25519PrivateKey.generate().public_key()
    with pytest.raises(jwt.InvalidSignatureError):
        _decodificar(firmar_identidad(USUARIO, "player"), otra)


def test_cambiar_el_contenido_rompe_la_firma():
    # Cambiar el sub de un token real (suplantar a otro) invalida la firma.
    encabezado, _, firma = firmar_identidad(USUARIO, "player").split(".")
    otro = jwt.utils.base64url_encode(
        f'{{"sub":"{uuid.uuid4()}","rol":"player","iss":"usuarios","aud":"polebet","iat":1,"exp":9999999999}}'.encode()
    ).decode()
    with pytest.raises(jwt.InvalidSignatureError):
        _decodificar(f"{encabezado}.{otro}.{firma}")


def test_rol_desconocido_no_se_firma():
    with pytest.raises(ValueError):
        firmar_identidad(USUARIO, "superusuario")
