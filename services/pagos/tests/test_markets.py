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


# --- HU2-10: liquidacion individual (lo que el Motor necesita de Pagos) ---


def _cerrar(mercado_id, ganadora="VER"):
    from app.db import SessionLocal
    from app.models import MARKET_CERRADO, Market

    with SessionLocal() as db:
        m = db.get(Market, mercado_id)
        m.status = MARKET_CERRADO
        m.winning_option = ganadora
        db.commit()


def _apostar(client, fabrica, mercado, opcion="VER", monto=100):
    user_id = fabrica.billetera(balance=1000)
    bet_id = uuid.uuid4()
    clave = f"bet-debit:{bet_id}"
    r = client.post(
        "/internal/debit",
        headers=headers(clave),
        json={
            "bet_id": str(bet_id),
            "user_id": str(user_id),
            "market_id": str(mercado.id),
            "option": opcion,
            "amount": monto,
            "multiplier": "2.5" if opcion == "VER" else "3",
            "lap": None,
            "idempotency_key": clave,
        },
    )
    assert r.status_code == 201, r.text
    return bet_id, user_id


@requires_db
def test_lista_solo_los_mercados_cerrados_con_ganador(client, fabrica):
    evento = fabrica.evento()
    listo = fabrica.mercado(evento.id)
    sin_ganador = fabrica.mercado(evento.id)
    abierto = fabrica.mercado(evento.id)
    _cerrar(listo.id)
    _cerrar(sin_ganador.id, ganadora=None)

    r = client.get("/internal/markets?status=cerrado&resuelto=true&limit=200", headers=headers())

    assert r.status_code == 200, r.text
    ids = {m["market_id"] for m in r.json()}
    assert str(listo.id) in ids
    assert str(sin_ganador.id) not in ids and str(abierto.id) not in ids
    assert next(m for m in r.json() if m["market_id"] == str(listo.id))["winning_option"] == "VER"


@requires_db
def test_apuestas_pendientes_individuales_del_mercado(client, fabrica):
    mercado = fabrica.mercado()
    bet_ver, user_ver = _apostar(client, fabrica, mercado, "VER", 100)
    bet_nor, _ = _apostar(client, fabrica, mercado, "NOR", 50)

    r = client.get(f"/internal/markets/{mercado.id}/bets?mode=individual&status=pending", headers=headers())

    assert r.status_code == 200, r.text
    por_id = {b["bet_id"]: b for b in r.json()}
    assert set(por_id) == {str(bet_ver), str(bet_nor)}
    b = por_id[str(bet_ver)]
    assert (b["user_id"], b["option"], b["amount"], b["multiplier"], b["status"], b["payout"]) == (
        str(user_ver), "VER", 100, 2.5, "pending", None,
    )
    assert client.get(f"/internal/markets/{uuid.uuid4()}/bets", headers=headers()).status_code == 404


@requires_db
def test_marcar_liquidado_solo_cuando_no_quedan_pendientes(client, fabrica):
    mercado = fabrica.mercado()
    bet_id, _ = _apostar(client, fabrica, mercado, "VER", 100)
    url = f"/internal/markets/{mercado.id}/liquidated"

    # Abierto/sin ganador: no se puede.
    assert client.post(url, headers=headers()).status_code == 409
    _cerrar(mercado.id)
    # Cerrado con ganador pero con una apuesta individual sin pagar: tampoco.
    assert client.post(url, headers=headers()).status_code == 409

    clave = f"bet-settlement:{bet_id}"
    r = client.post("/internal/settlements", headers=headers(clave), json={"bet_id": str(bet_id), "result": "won", "idempotency_key": clave})
    assert r.status_code == 201, r.text

    r = client.post(url, headers=headers())
    assert r.status_code == 200 and r.json() == {"market_id": str(mercado.id), "status": "liquidado", "nueva": True}
    # Repetir no cambia nada.
    assert client.post(url, headers=headers()).json()["nueva"] is False
    assert client.get(f"/internal/markets/{mercado.id}", headers=headers()).json()["status"] == "liquidado"
    assert client.post(f"/internal/markets/{uuid.uuid4()}/liquidated", headers=headers()).status_code == 404