"""Pruebas de HU2-09 (parte Pagos): lectura de un mercado para el Motor."""
import uuid

from tests.conftest import headers, requires_db


@requires_db
def test_el_motor_lee_el_mercado_con_sus_opciones_y_multiplicadores(client, fabrica):
    mercado = fabrica.mercado()

    r = client.get(f"/internal/markets/{mercado.id}", headers=headers())

    assert r.status_code == 200, r.text
    m = r.json()
    assert (m["market_id"], m["event_id"], m["kind"], m["status"]) == (str(mercado.id), str(mercado.event_id), "estatica", "programado")
    assert m["options"] == [{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3.0}]
    assert r.headers["cache-control"] == "no-store"


@requires_db
def test_mercado_inexistente_y_token(client):
    assert client.get(f"/internal/markets/{uuid.uuid4()}", headers=headers()).status_code == 404
    assert client.get(f"/internal/markets/{uuid.uuid4()}").status_code == 401
