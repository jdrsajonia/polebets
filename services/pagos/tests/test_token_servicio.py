"""Escenario 4 (T3-05): Pagos acepta solo el token del Orquestador y el del Motor. Sin BD."""
import pytest

from app.core.config import Settings
from app.core.internal_auth import servicio_que_llama, token_servicio_valido
from tests.conftest import TOKEN, TOKEN_MOTOR


def test_identifica_a_quien_llama():
    assert servicio_que_llama(TOKEN) == "orquestador"
    assert servicio_que_llama(TOKEN_MOTOR) == "motor"


@pytest.mark.parametrize(
    "token",
    [
        None,
        "",
        "token-del-orquestador-hacia-usuarios-32-car",  # el de otro destino
        TOKEN + "x",
        TOKEN_MOTOR[:-1],
        "carácter-no-ascii-que-falla",
    ],
)
def test_rechaza_cualquier_otro_token(token):
    assert servicio_que_llama(token) is None
    assert not token_servicio_valido(token)


def _settings(**tokens):
    """Settings sin leer el entorno ni el .env, solo con los tokens dados."""
    return Settings(_env_file=None, **tokens)


def test_la_app_http_exige_los_dos_tokens():
    with pytest.raises(ValueError, match="TOKEN_DESDE_MOTOR"):
        _settings(token_desde_orquestador=TOKEN, token_desde_motor="").tokens_servicio_configurados()
    with pytest.raises(ValueError, match="TOKEN_DESDE_ORQUESTADOR"):
        _settings(token_desde_orquestador="", token_desde_motor=TOKEN_MOTOR).tokens_servicio_configurados()
    _settings(token_desde_orquestador=TOKEN, token_desde_motor=TOKEN_MOTOR).tokens_servicio_configurados()


def test_los_dos_tokens_no_pueden_ser_iguales():
    with pytest.raises(ValueError, match="mismo valor"):
        _settings(token_desde_orquestador=TOKEN, token_desde_motor=TOKEN).tokens_servicio_configurados()


def test_token_corto_se_rechaza():
    with pytest.raises(ValueError):
        _settings(token_desde_orquestador="corto", token_desde_motor=TOKEN_MOTOR)


def test_el_consumidor_arranca_sin_tokens():
    # pagos-consumer no recibe HTTP: la configuracion no le exige tokens.
    s = _settings(token_desde_orquestador="", token_desde_motor="")
    assert s.token_desde_orquestador == "" and s.token_desde_motor == ""
