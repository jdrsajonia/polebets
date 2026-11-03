"""Apuestas de torneo en Pagos (HU2-09, HU2-10): registro y liquidación sin tokens ni ledger."""
import threading
import uuid
from decimal import Decimal

from sqlalchemy import select

from app.db import SessionLocal
from app.models import MARKET_CERRADO, Bet, LedgerEntry, Market
from app.services.billetera import saldo_de
from app.services.liquidacion import ApuestaDuplicada, BetDebitData, registrar_apuesta_torneo
from tests.conftest import headers, requires_db

MERCADO_CERRADO = {"codigo": "mercado_cerrado", "mensaje": "El mercado no está abierto"}


def _registrar(client, user_id, mercado, amount=100, multiplier="2.5", option="VER", lap=None, bet_id=None):
    bet_id = bet_id or uuid.uuid4()
    r = client.post(
        "/internal/bets/torneo",
        headers=headers(),
        json={
            "bet_id": str(bet_id),
            "user_id": str(user_id),
            "market_id": str(mercado.id),
            "option": option,
            "amount": amount,
            "multiplier": multiplier,
            "lap": lap,
        },
    )
    return r, bet_id


def _liquidar(client, bet_id, result):
    return client.post(f"/internal/bets/torneo/{bet_id}/settlement", headers=headers(), json={"result": result})


def _debitar(client, user_id, mercado, amount=10):
    bet_id = uuid.uuid4()
    clave = f"bet-debit:{bet_id}"
    return client.post(
        "/internal/debit",
        headers=headers(clave),
        json={
            "bet_id": str(bet_id),
            "user_id": str(user_id),
            "market_id": str(mercado.id),
            "option": "VER",
            "amount": amount,
            "multiplier": "2.5",
            "lap": None,
            "idempotency_key": clave,
        },
    ), bet_id


def _cerrar(mercado_id, ganadora=None):
    with SessionLocal() as db:
        m = db.get(Market, mercado_id)
        m.status = MARKET_CERRADO
        m.winning_option = ganadora
        db.commit()


def _saldo(user_id):
    with SessionLocal() as db:
        return saldo_de(db, user_id)


def _ledger(user_id):
    with SessionLocal() as db:
        return db.scalars(select(LedgerEntry).where(LedgerEntry.user_id == user_id)).all()


# ---------------------------------------------------------------------------
# Registro
# ---------------------------------------------------------------------------
@requires_db
def test_registrar_guarda_la_apuesta_de_torneo_sin_tocar_tokens(client, fabrica):
    user_id = fabrica.billetera(balance=50)
    mercado = fabrica.mercado()

    r, bet_id = _registrar(client, user_id, mercado, amount=300, lap=7)

    assert r.status_code == 201, r.text
    assert r.json() == {"bet_id": str(bet_id), "event_id": str(mercado.event_id), "status": "pending", "payout": None, "nueva": True}
    with SessionLocal() as db:
        a = db.get(Bet, bet_id)
        assert (a.mode, a.amount, a.multiplier, a.lap, a.option) == ("torneo", 300, Decimal("2.50"), 7, "VER")
    assert _saldo(user_id) == 50
    assert _ledger(user_id) == []


@requires_db
def test_registrar_es_idempotente_por_bet_id(client, fabrica):
    user_id = fabrica.billetera()
    mercado = fabrica.mercado()

    primera, bet_id = _registrar(client, user_id, mercado, lap=3)
    reenvio, _ = _registrar(client, user_id, mercado, lap=3, bet_id=bet_id)

    assert (primera.status_code, reenvio.status_code) == (201, 200)
    assert reenvio.json()["nueva"] is False


@requires_db
def test_reenvio_con_otra_vuelta_es_200_y_conserva_la_primera(client, fabrica):
    user_id = fabrica.billetera()
    mercado = fabrica.mercado()
    _, bet_id = _registrar(client, user_id, mercado, lap=3)

    r, _ = _registrar(client, user_id, mercado, lap=5, bet_id=bet_id)

    assert (r.status_code, r.json()["nueva"]) == (200, False)
    with SessionLocal() as db:
        assert db.get(Bet, bet_id).lap == 3


@requires_db
def test_registrar_con_datos_distintos_devuelve_409(client, fabrica):
    user_id = fabrica.billetera()
    mercado = fabrica.mercado()
    _, bet_id = _registrar(client, user_id, mercado, amount=100, lap=3)

    for cambio in ({"amount": 101}, {"option": "NOR"}, {"multiplier": "3"}):
        r, _ = _registrar(client, user_id, mercado, **{"amount": 100, "lap": 3, **cambio}, bet_id=bet_id)
        assert r.status_code == 409, cambio
        assert r.json()["detail"]["codigo"] == "datos_distintos"


@requires_db
def test_registrar_un_bet_id_individual_devuelve_409(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    mercado = fabrica.mercado()
    d, bet_id = _debitar(client, user_id, mercado)
    assert d.status_code == 201

    r, _ = _registrar(client, user_id, mercado, amount=10, bet_id=bet_id)
    assert (r.status_code, r.json()["detail"]["codigo"]) == (409, "datos_distintos")


@requires_db
def test_una_apuesta_por_usuario_y_mercado_en_cualquier_modo(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    torneo, individual = fabrica.mercado(), fabrica.mercado()

    assert _registrar(client, user_id, torneo)[0].status_code == 201
    r, _ = _registrar(client, user_id, torneo)  # otro bet_id, mismo mercado
    assert (r.status_code, r.json()["detail"]["codigo"]) == (409, "ya_apostaste")
    # Tampoco en individual sobre el mismo mercado.
    d, _ = _debitar(client, user_id, torneo)
    assert (d.status_code, d.json()["detail"]) == (409, "Ya apostó en este mercado")
    assert _saldo(user_id) == 100

    # Y al revés: ya apostó en individual, no puede en torneo.
    assert _debitar(client, user_id, individual)[0].status_code == 201
    r, _ = _registrar(client, user_id, individual)
    assert (r.status_code, r.json()["detail"]["codigo"]) == (409, "ya_apostaste")


@requires_db
def test_dos_registros_simultaneos_del_mismo_usuario_solo_acepta_uno(fabrica):
    user_id = fabrica.billetera()
    mercado = fabrica.mercado()
    barrera = threading.Barrier(2)
    aceptadas, duplicadas, errores = [], [], []

    def registrar():
        bet_id = uuid.uuid4()
        datos = BetDebitData(bet_id, user_id, mercado.id, "VER", 10, Decimal("2"), None, str(bet_id))
        try:
            with SessionLocal() as db:
                barrera.wait()
                aceptadas.append(registrar_apuesta_torneo(db, datos))
        except ApuestaDuplicada:
            duplicadas.append(bet_id)
        except Exception as error:  # noqa: BLE001  (se revisa abajo)
            errores.append(error)

    hilos = [threading.Thread(target=registrar) for _ in range(2)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    assert errores == []
    assert (len(aceptadas), len(duplicadas)) == (1, 1)


@requires_db
def test_registrar_en_mercado_inexistente_devuelve_404(client, fabrica):
    falso = type(fabrica.mercado())(id=uuid.uuid4())
    r, _ = _registrar(client, fabrica.billetera(), falso)
    assert r.status_code == 404


@requires_db
def test_registrar_exige_token(client, fabrica):
    mercado = fabrica.mercado()
    r = client.post(
        "/internal/bets/torneo",
        json={"bet_id": str(uuid.uuid4()), "user_id": str(uuid.uuid4()), "market_id": str(mercado.id),
              "option": "VER", "amount": 1, "multiplier": "2"},
    )
    assert r.status_code == 401


# ---------------------------------------------------------------------------
# Mercado no abierto (RNF-19), en los dos modos
# ---------------------------------------------------------------------------
@requires_db
def test_mercado_no_abierto_rechaza_la_apuesta_de_torneo(client, fabrica):
    user_id = fabrica.billetera()
    for estado in ("programado", "cerrado", "liquidado"):
        mercado = fabrica.mercado(status=estado)
        r, bet_id = _registrar(client, user_id, mercado)
        assert (r.status_code, r.json()["detail"]) == (409, MERCADO_CERRADO), estado
        with SessionLocal() as db:
            assert db.get(Bet, bet_id) is None


@requires_db
def test_mercado_no_abierto_rechaza_el_debito_sin_cobrar(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    for estado in ("programado", "cerrado", "liquidado"):
        r, bet_id = _debitar(client, user_id, fabrica.mercado(status=estado))
        assert (r.status_code, r.json()["detail"]) == (409, MERCADO_CERRADO), estado
        with SessionLocal() as db:
            assert db.get(Bet, bet_id) is None
    assert _saldo(user_id) == 100
    assert _ledger(user_id) == []


@requires_db
def test_reenvio_tras_cerrar_el_mercado_sigue_siendo_200(client, fabrica):
    """El reintento de una apuesta ya registrada no depende del estado del mercado."""
    user_id = fabrica.billetera(balance=100)
    mercado, otro = fabrica.mercado(), fabrica.mercado()
    _, bet_torneo = _registrar(client, user_id, mercado, amount=10)
    d, _ = _debitar(client, user_id, otro, amount=10)
    assert d.status_code == 201
    _cerrar(mercado.id)

    r, _ = _registrar(client, user_id, mercado, amount=10, bet_id=bet_torneo)
    assert r.status_code == 200


# ---------------------------------------------------------------------------
# Liquidación
# ---------------------------------------------------------------------------
@requires_db
def test_liquidar_marca_el_resultado_y_el_pago_en_fichas_sin_ledger(client, fabrica):
    user_id = fabrica.billetera(balance=7)
    gana, pierde = fabrica.mercado(), fabrica.mercado()
    _, gana_id = _registrar(client, user_id, gana, amount=101, multiplier="2.5")
    _, pierde_id = _registrar(client, user_id, pierde, amount=40)

    won = _liquidar(client, gana_id, "won")
    lost = _liquidar(client, pierde_id, "lost")

    assert won.status_code == 201, won.text
    assert (won.json()["status"], won.json()["payout"], won.json()["nueva"]) == ("won", 252, True)  # 252,5 -> 252
    assert (lost.status_code, lost.json()["status"], lost.json()["payout"]) == (201, "lost", 0)
    assert _saldo(user_id) == 7
    assert _ledger(user_id) == []


@requires_db
def test_liquidar_es_idempotente_y_otro_resultado_es_409(client, fabrica):
    _, bet_id = _registrar(client, fabrica.billetera(), fabrica.mercado(), amount=10, multiplier="3")

    primera = _liquidar(client, bet_id, "won")
    reenvio = _liquidar(client, bet_id, "won")
    contraria = _liquidar(client, bet_id, "lost")

    assert (primera.status_code, reenvio.status_code, contraria.status_code) == (201, 200, 409)
    assert reenvio.json() == {**primera.json(), "nueva": False}
    assert contraria.json()["detail"]["codigo"] == "datos_distintos"
    with SessionLocal() as db:
        assert (db.get(Bet, bet_id).status, db.get(Bet, bet_id).payout) == ("won", 30)


@requires_db
def test_liquidar_torneo_no_acepta_individuales_ni_inexistentes(client, fabrica):
    user_id = fabrica.billetera(balance=10)
    _, bet_id = _debitar(client, user_id, fabrica.mercado())

    assert _liquidar(client, bet_id, "won").status_code == 422
    assert _liquidar(client, uuid.uuid4(), "won").status_code == 404
    assert _liquidar(client, bet_id, "pending").status_code == 422
    with SessionLocal() as db:
        assert db.get(Bet, bet_id).status == "pending"


# ---------------------------------------------------------------------------
# /liquidated con pendientes de torneo
# ---------------------------------------------------------------------------
@requires_db
def test_liquidated_espera_tambien_a_las_apuestas_de_torneo(client, fabrica):
    mercado = fabrica.mercado()
    individual_user = fabrica.billetera(balance=100)
    d, bet_ind = _debitar(client, individual_user, mercado)
    _, bet_tor = _registrar(client, fabrica.billetera(), mercado, amount=50)
    _cerrar(mercado.id, ganadora="VER")
    url = f"/internal/markets/{mercado.id}/liquidated"

    clave = f"bet-settlement:{bet_ind}"
    r = client.post("/internal/settlements", headers=headers(clave),
                    json={"bet_id": str(bet_ind), "result": "won", "idempotency_key": clave})
    assert r.status_code == 201, r.text

    # Ya no quedan individuales, pero sí una de torneo.
    r = client.post(url, headers=headers())
    assert (r.status_code, r.json()["detail"]) == (409, "Quedan apuestas pendientes")

    assert _liquidar(client, bet_tor, "won").status_code == 201
    r = client.post(url, headers=headers())
    assert r.status_code == 200 and r.json()["nueva"] is True


@requires_db
def test_el_motor_lista_las_pendientes_de_torneo_con_su_modo(client, fabrica):
    mercado = fabrica.mercado()
    _debitar(client, fabrica.billetera(balance=100), mercado)
    _, bet_tor = _registrar(client, fabrica.billetera(), mercado, amount=50)

    r = client.get(f"/internal/markets/{mercado.id}/bets?mode=torneo&status=pending", headers=headers())

    assert r.status_code == 200
    assert [(b["bet_id"], b["mode"], b["amount"]) for b in r.json()] == [(str(bet_tor), "torneo", 50)]


@requires_db
def test_pago_en_fichas_cuadra_con_el_motor(client, fabrica):
    """Mismo calculo que fichas.FichasPagadas del Motor: monto × centésimas / 100, truncado."""
    user_id = fabrica.billetera()
    for monto, multiplicador, esperado in ((10, "2.30", 23), (100, "1.13", 113), (50, "1.14", 57)):
        _, bet_id = _registrar(client, user_id, fabrica.mercado(), amount=monto, multiplier=multiplicador)
        assert _liquidar(client, bet_id, "won").json()["payout"] == esperado, (monto, multiplicador)
