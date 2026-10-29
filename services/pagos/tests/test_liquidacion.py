"""Débito y liquidación de apuestas individuales contra wallets (T2-03)."""
import threading
import uuid
from decimal import Decimal

import pytest
from pydantic import ValidationError
from sqlalchemy import select

from app.core.internal_auth import token_servicio_valido
from app.db import SessionLocal
from app.models import (
    BET_LOST,
    BET_PENDING,
    BET_TORNEO,
    BET_WON,
    LEDGER_DEBIT,
    LEDGER_SETTLE,
    Bet,
    LedgerEntry,
)
from app.schemas.internal import MAX_BET_AMOUNT, BetDebitIn, BetSettlementIn
from app.services.billetera import saldo_de
from app.services.liquidacion import (
    BetDebitData,
    SaldoInsuficiente,
    debitar_apuesta,
    liquidar_apuesta,
    pago_por_apuesta,
)
from tests.conftest import TOKEN, headers, requires_db


def _debitar(client, user_id, mercado, amount, multiplier="2.5", bet_id=None, lap=None):
    bet_id = bet_id or uuid.uuid4()
    clave = f"bet-debit:{bet_id}"
    response = client.post(
        "/internal/debit",
        json={
            "bet_id": str(bet_id),
            "user_id": str(user_id),
            "market_id": str(mercado.id),
            "option": "VER",
            "amount": amount,
            "multiplier": multiplier,
            "lap": lap,
            "idempotency_key": clave,
        },
        headers=headers(clave),
    )
    return response, bet_id


def _liquidar(client, bet_id, result):
    clave = f"bet-settlement:{bet_id}"
    return client.post(
        "/internal/settlements",
        json={"bet_id": str(bet_id), "result": result, "idempotency_key": clave},
        headers=headers(clave),
    )


def _saldo(user_id):
    with SessionLocal() as db:
        return saldo_de(db, user_id)


def _ledger(user_id):
    with SessionLocal() as db:
        return db.scalars(select(LedgerEntry).where(LedgerEntry.user_id == user_id)).all()


# ---------------------------------------------------------------------------
# Contratos (sin BD)
# ---------------------------------------------------------------------------
def test_token_de_servicio():
    assert token_servicio_valido(TOKEN)
    for token in (None, "incorrecto", "carácter-no-ascii-que-falla"):
        assert not token_servicio_valido(token)


def test_contrato_de_debito():
    base = {
        "bet_id": str(uuid.uuid4()),
        "user_id": str(uuid.uuid4()),
        "market_id": str(uuid.uuid4()),
        "option": "VER",
        "amount": MAX_BET_AMOUNT,
        "multiplier": "2.50",
        "idempotency_key": "bet-debit:x",
    }
    assert BetDebitIn.model_validate(base).lap is None
    for invalido in (
        {"amount": MAX_BET_AMOUNT + 1},
        {"amount": 0},
        {"multiplier": "0"},
        {"multiplier": "1.255"},
        {"market_id": "market-demo"},  # en el P2 el mercado es una fila de markets
        {"lap": -1},
        {"points": 10},
    ):
        with pytest.raises(ValidationError):
            BetDebitIn.model_validate({**base, **invalido})


def test_contrato_de_liquidacion_sin_puntos():
    base = {"bet_id": str(uuid.uuid4()), "result": "won", "idempotency_key": "bet-settlement:x"}
    assert BetSettlementIn.model_validate(base).result == "won"
    for invalido in ({"result": "pending"}, {"points": "10"}):
        with pytest.raises(ValidationError):
            BetSettlementIn.model_validate({**base, **invalido})


def test_pago_es_monto_por_multiplicador_en_tokens_enteros():
    assert pago_por_apuesta(100, Decimal("2.5")) == 250
    assert pago_por_apuesta(3, Decimal("1.5")) == 4  # 4,5 -> 4
    assert pago_por_apuesta(10, Decimal("1")) == 10


# ---------------------------------------------------------------------------
# Débito (RF-12, RNF-11)
# ---------------------------------------------------------------------------
@requires_db
def test_debito_registra_la_apuesta_y_el_movimiento_en_una_transaccion(client, fabrica):
    user_id = fabrica.billetera(balance=200)
    mercado = fabrica.mercado()

    r, bet_id = _debitar(client, user_id, mercado, 80, lap=12)
    assert r.status_code == 201
    assert r.json() == {
        "bet_id": str(bet_id),
        "event_id": str(mercado.event_id),
        "status": BET_PENDING,
        "saldo": 120,
        "nueva": True,
    }

    with SessionLocal() as db:
        apuesta = db.get(Bet, bet_id)
        assert (apuesta.mode, apuesta.multiplier, apuesta.lap, apuesta.payout) == ("individual", Decimal("2.50"), 12, None)
        assert apuesta.event_id == mercado.event_id
    [movimiento] = _ledger(user_id)
    assert (movimiento.type, movimiento.tokens_delta, movimiento.bet_id, movimiento.event_id) == (
        LEDGER_DEBIT,
        -80,
        bet_id,
        mercado.event_id,
    )


@requires_db
def test_reenvio_del_debito_no_cobra_dos_veces(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    mercado = fabrica.mercado()

    primera, bet_id = _debitar(client, user_id, mercado, 30)
    segunda, _ = _debitar(client, user_id, mercado, 30, bet_id=bet_id)
    otra_cosa, _ = _debitar(client, user_id, mercado, 31, bet_id=bet_id)

    assert (primera.status_code, segunda.status_code, otra_cosa.status_code) == (201, 200, 409)
    assert _saldo(user_id) == 70
    assert len(_ledger(user_id)) == 1


@requires_db
def test_una_sola_apuesta_por_usuario_y_mercado(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    mercado = fabrica.mercado()

    assert _debitar(client, user_id, mercado, 10)[0].status_code == 201
    r, _ = _debitar(client, user_id, mercado, 10)  # otra apuesta, otro bet_id
    assert r.status_code == 409
    assert r.json() == {"detail": "Ya apostó en este mercado"}
    assert _saldo(user_id) == 90


@requires_db
def test_saldo_insuficiente_no_cambia_nada(client, fabrica):
    user_id = fabrica.billetera(balance=10)
    mercado = fabrica.mercado()

    r, bet_id = _debitar(client, user_id, mercado, 11)
    assert r.status_code == 422
    assert _saldo(user_id) == 10
    assert _ledger(user_id) == []
    with SessionLocal() as db:
        assert db.get(Bet, bet_id) is None


@requires_db
def test_mercado_o_billetera_inexistente_responde_404(client, fabrica):
    mercado = fabrica.mercado()
    falso = type(mercado)(id=uuid.uuid4())

    r, _ = _debitar(client, fabrica.billetera(balance=10), falso, 5)
    assert (r.status_code, r.json()["detail"]) == (404, "Mercado inexistente")
    r, _ = _debitar(client, fabrica.usuario_sin_billetera(), mercado, 5)
    assert (r.status_code, r.json()["detail"]) == (404, "Billetera inexistente")


@requires_db
def test_dos_apuestas_simultaneas_que_superan_el_saldo_solo_acepta_una(fabrica):
    """Criterio de RNF-11."""
    user_id = fabrica.billetera(balance=100)
    mercados = [fabrica.mercado(), fabrica.mercado()]
    barrera = threading.Barrier(2)
    aceptadas, rechazadas, errores = [], [], []

    def apostar(mercado):
        bet_id = uuid.uuid4()
        datos = BetDebitData(
            bet_id=bet_id,
            user_id=user_id,
            market_id=mercado.id,
            option="VER",
            amount=70,
            multiplier=Decimal("2"),
            lap=None,
            idempotency_key=f"bet-debit:{bet_id}",
        )
        try:
            with SessionLocal() as db:
                barrera.wait()
                aceptadas.append(debitar_apuesta(db, datos))
        except SaldoInsuficiente:
            rechazadas.append(bet_id)
        except Exception as error:  # noqa: BLE001  (se revisa abajo)
            errores.append(error)

    hilos = [threading.Thread(target=apostar, args=(m,)) for m in mercados]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    assert errores == []
    assert (len(aceptadas), len(rechazadas)) == (1, 1)
    assert _saldo(user_id) == 30


# ---------------------------------------------------------------------------
# Liquidación individual (RF-14, RNF-12)
# ---------------------------------------------------------------------------
@requires_db
def test_liquidacion_paga_monto_por_multiplicador_o_nada(client, fabrica):
    user_id = fabrica.billetera(balance=200)
    ganadora, perdedora = fabrica.mercado(), fabrica.mercado()

    _, gana_id = _debitar(client, user_id, ganadora, 100, multiplier="2.5")
    _, pierde_id = _debitar(client, user_id, perdedora, 20)
    assert _saldo(user_id) == 80

    won = _liquidar(client, gana_id, BET_WON)
    lost = _liquidar(client, pierde_id, BET_LOST)
    assert won.status_code == 201
    assert won.json() == {"bet_id": str(gana_id), "status": BET_WON, "payout": 250, "saldo": 330, "nueva": True}
    assert lost.status_code == 201
    assert (lost.json()["payout"], lost.json()["saldo"]) == (0, 330)

    with SessionLocal() as db:
        assert (db.get(Bet, gana_id).payout, db.get(Bet, pierde_id).payout) == (250, 0)
    liquidaciones = {m.bet_id: m.tokens_delta for m in _ledger(user_id) if m.type == LEDGER_SETTLE}
    assert liquidaciones == {gana_id: 250, pierde_id: 0}


@requires_db
def test_reenvio_de_la_liquidacion_no_paga_doble(client, fabrica):
    user_id = fabrica.billetera(balance=100)
    _, bet_id = _debitar(client, user_id, fabrica.mercado(), 100, multiplier="2")

    primera = _liquidar(client, bet_id, BET_WON)
    reenvio = _liquidar(client, bet_id, BET_WON)
    contraria = _liquidar(client, bet_id, BET_LOST)

    assert (primera.status_code, reenvio.status_code, contraria.status_code) == (201, 200, 409)
    assert reenvio.json()["nueva"] is False
    assert _saldo(user_id) == 200


@requires_db
def test_dos_liquidaciones_simultaneas_pagan_una_vez(client, fabrica):
    user_id = fabrica.billetera(balance=50)
    _, bet_id = _debitar(client, user_id, fabrica.mercado(), 50, multiplier="3")
    clave = f"bet-settlement:{bet_id}"
    barrera = threading.Barrier(2)
    resultados, errores = [], []

    def liquidar():
        try:
            with SessionLocal() as db:
                barrera.wait()
                resultados.append(liquidar_apuesta(db, bet_id, BET_WON, clave))
        except Exception as error:  # noqa: BLE001  (se revisa abajo)
            errores.append(error)

    hilos = [threading.Thread(target=liquidar) for _ in range(2)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    assert errores == []
    assert sorted(r.nueva for r in resultados) == [False, True]
    assert _saldo(user_id) == 150
    assert sum(m.type == LEDGER_SETTLE for m in _ledger(user_id)) == 1


@requires_db
def test_apuesta_de_torneo_no_se_liquida_en_tokens(client, fabrica):
    user_id = fabrica.billetera(balance=0)
    mercado = fabrica.mercado()
    bet_id = uuid.uuid4()
    with SessionLocal() as db:
        db.add(
            Bet(
                id=bet_id,
                user_id=user_id,
                market_id=mercado.id,
                event_id=mercado.event_id,
                mode=BET_TORNEO,
                option="VER",
                amount=300,
                multiplier=Decimal("2"),
            )
        )
        db.commit()

    r = _liquidar(client, bet_id, BET_WON)
    assert r.status_code == 422
    assert _saldo(user_id) == 0
    assert _ledger(user_id) == []


@requires_db
def test_sin_token_o_con_clave_invalida_no_cambia_la_apuesta(client, fabrica):
    user_id = fabrica.billetera(balance=10)
    _, bet_id = _debitar(client, user_id, fabrica.mercado(), 10)

    sin_token = client.post(
        "/internal/settlements",
        json={"bet_id": str(bet_id), "result": "won", "idempotency_key": f"bet-settlement:{bet_id}"},
        headers={"Idempotency-Key": f"bet-settlement:{bet_id}"},
    )
    clave_mala = client.post(
        "/internal/settlements",
        json={"bet_id": str(bet_id), "result": "won", "idempotency_key": "otra"},
        headers=headers("otra"),
    )
    assert (sin_token.status_code, clave_mala.status_code) == (401, 400)
    with SessionLocal() as db:
        assert db.get(Bet, bet_id).status == BET_PENDING
    assert _saldo(user_id) == 0
