"""Restricciones de la base de Pagos (T2-03, Vista C&C prototipo 2).

Cada prueba intenta guardar algo que el modelo prohíbe y espera que
PostgreSQL lo rechace, no solo el código de la aplicación.
"""
import uuid
from decimal import Decimal

import pytest
from sqlalchemy.exc import IntegrityError

from app.db import SessionLocal
from app.models import (
    BET_INDIVIDUAL,
    LEDGER_PURCHASE,
    Bet,
    EventEntry,
    LedgerEntry,
    Wallet,
)
from tests.conftest import requires_db


def _rechaza(*objetos):
    with SessionLocal() as db:
        db.add_all(objetos)
        with pytest.raises(IntegrityError):
            db.commit()


def _apuesta(user_id, mercado, **cambios):
    datos = dict(
        id=uuid.uuid4(),
        user_id=user_id,
        market_id=mercado.id,
        event_id=mercado.event_id,
        mode=BET_INDIVIDUAL,
        option="VER",
        amount=10,
        multiplier=Decimal("2.5"),
    )
    datos.update(cambios)
    return Bet(**datos)


@requires_db
def test_saldo_nunca_queda_negativo(fabrica):
    _rechaza(Wallet(user_id=fabrica.usuario_sin_billetera(), balance=-1))


@requires_db
def test_una_billetera_por_usuario(fabrica):
    user_id = fabrica.billetera()
    _rechaza(Wallet(user_id=user_id))


@requires_db
def test_estados_del_evento_y_del_torneo_son_los_del_documento(fabrica):
    for cambio in ({"status": "registration"}, {"tournament_status": "multiplayer"}):
        evento = fabrica.evento()
        with SessionLocal() as db:
            obj = db.get(type(evento), evento.id)
            for campo, valor in cambio.items():
                setattr(obj, campo, valor)
            with pytest.raises(IntegrityError):
                db.commit()


@requires_db
def test_una_sola_inscripcion_por_evento(fabrica):
    evento = fabrica.evento()
    user_id = uuid.uuid4()
    with SessionLocal() as db:
        db.add(EventEntry(event_id=evento.id, user_id=user_id))
        db.commit()
    _rechaza(EventEntry(event_id=evento.id, user_id=user_id))


@requires_db
def test_una_sola_apuesta_por_usuario_y_mercado(fabrica):
    user_id = fabrica.billetera()
    mercado = fabrica.mercado()
    with SessionLocal() as db:
        db.add(_apuesta(user_id, mercado))
        db.commit()
    _rechaza(_apuesta(user_id, mercado, option="NOR"))


@requires_db
def test_la_apuesta_exige_un_mercado_que_exista(fabrica):
    mercado = fabrica.mercado()
    falso = type(mercado)(id=uuid.uuid4(), event_id=mercado.event_id)
    _rechaza(_apuesta(fabrica.billetera(), falso))


@requires_db
def test_modo_y_monto_de_la_apuesta(fabrica):
    mercado = fabrica.mercado()
    for cambio in ({"mode": "mixto"}, {"amount": 0}, {"multiplier": Decimal("0")}):
        _rechaza(_apuesta(fabrica.billetera(), mercado, **cambio))


@requires_db
def test_tipos_del_ledger_y_clave_unica(fabrica):
    user_id = fabrica.billetera()
    _rechaza(LedgerEntry(user_id=user_id, type="bet_debit", tokens_delta=-1, idempotency_key=f"x:{uuid.uuid4()}"))

    clave = f"purchase:{user_id}:{uuid.uuid4()}"
    with SessionLocal() as db:
        db.add(LedgerEntry(user_id=user_id, type=LEDGER_PURCHASE, tokens_delta=5, idempotency_key=clave))
        db.commit()
    _rechaza(LedgerEntry(user_id=user_id, type=LEDGER_PURCHASE, tokens_delta=5, idempotency_key=clave))
