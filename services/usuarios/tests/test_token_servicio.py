"""Escenario 4 (T3-05): Usuarios solo acepta el token del Orquestador. Sin BD."""
import pytest

from app.core.internal_auth import token_servicio_valido
from tests.conftest import TOKEN_SERVICIO


def test_acepta_el_token_del_orquestador():
    assert token_servicio_valido(TOKEN_SERVICIO)


@pytest.mark.parametrize(
    "token",
    [
        None,
        "",
        "token-del-motor-hacia-pagos-de-32-caracteres",  # el de otro servicio
        TOKEN_SERVICIO + "x",
        TOKEN_SERVICIO[:-1],
        "carácter-no-ascii-que-falla",
    ],
)
def test_rechaza_cualquier_otro_token(token):
    assert not token_servicio_valido(token)
