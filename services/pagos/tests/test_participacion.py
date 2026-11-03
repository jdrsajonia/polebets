"""HU2-22 (RF-28): participacion por modo, una fila por evento."""
import json
import uuid
from datetime import datetime, timedelta, timezone
from decimal import Decimal

from sqlalchemy import update

from app.db import SessionLocal
from app.models import (
    BET_INDIVIDUAL,
    BET_LOST,
    BET_PENDING,
    LEDGER_BUY_IN,
    LEDGER_DEBIT,
    LEDGER_PRIZE,
    LEDGER_REFUND,
    LEDGER_SETTLE,
    TORNEO_CANCELADO,
    TORNEO_REPARTIDO,
    Bet,
    Event,
    EventEntry,
    LedgerEntry,
)
from app.services.consulta_handler import procesar_consulta
from app.services.participacion import calcular_participacion
from app.services.resumen_financiero import FiltrosResumen, calcular_resumen
from tests.conftest import requires_db


def _participacion(**filtros):
    with SessionLocal() as db:
        return calcular_participacion(db, FiltrosResumen(**filtros))


def _movimiento(user_id, tipo, tokens, event_id, bet_id=None):
    with SessionLocal() as db:
        db.add(LedgerEntry(user_id=user_id, event_id=event_id, bet_id=bet_id, type=tipo,
                           tokens_delta=tokens, idempotency_key=f"prueba:{uuid.uuid4()}"))
        db.commit()


def _evento(fabrica, tournament_status, pot):
    evento = fabrica.evento()
    with SessionLocal() as db:
        db.execute(update(Event).where(Event.id == evento.id).values(tournament_status=tournament_status, pot=pot))
        db.commit()
    return evento.id


def _inscribir(event_id, user_id):
    with SessionLocal() as db:
        db.add(EventEntry(event_id=event_id, user_id=user_id))
        db.commit()
    _movimiento(user_id, LEDGER_BUY_IN, -100, event_id)


def _apuesta(user_id, market, amount, status, payout=None):
    bet_id = uuid.uuid4()
    with SessionLocal() as db:
        db.add(Bet(id=bet_id, user_id=user_id, market_id=market.id, event_id=market.event_id, mode=BET_INDIVIDUAL,
                   option="VER", amount=amount, multiplier=Decimal("2"), status=status, payout=payout))
        db.commit()
    _movimiento(user_id, LEDGER_DEBIT, -amount, market.event_id, bet_id)
    if status != BET_PENDING:
        _movimiento(user_id, LEDGER_SETTLE, payout, market.event_id, bet_id)


def _sembrar(fabrica):
    """Evento repartido: 4 inscritos (botin 400, premios 360) y 2 jugadores solo
    individuales. Un inscrito tambien hace una apuesta estatica: no es 'solo individual'."""
    jugadores = [fabrica.billetera() for _ in range(6)]
    event_id = _evento(fabrica, TORNEO_REPARTIDO, 400)
    for j in jugadores[:4]:
        _inscribir(event_id, j)
    _movimiento(jugadores[0], LEDGER_PRIZE, 360, event_id)
    mercado = fabrica.mercado(event_id)
    _apuesta(jugadores[0], mercado, 10, BET_LOST, 0)  # inscrito
    _apuesta(jugadores[4], mercado, 50, BET_LOST, 0)
    _apuesta(jugadores[5], mercado, 30, BET_PENDING)
    return event_id


@requires_db
def test_fila_de_un_evento(fabrica):
    event_id = _sembrar(fabrica)

    [fila] = _participacion(event_id=event_id)
    assert (fila.event_id, fila.tournament_status) == (event_id, TORNEO_REPARTIDO)
    assert (fila.inscritos, fila.solo_individuales, fila.botin) == (4, 2, 400)
    assert (fila.ganancia_torneo, fila.ganancia_individual, fila.apostado_pendiente) == (40, 90, 30)


@requires_db
def test_la_ganancia_coincide_con_el_resumen_financiero(fabrica):
    event_id = _sembrar(fabrica)

    [fila] = _participacion(event_id=event_id)
    with SessionLocal() as db:
        resumen = calcular_resumen(db, FiltrosResumen(event_id=event_id))
    assert fila.ganancia_torneo + fila.ganancia_individual == resumen.ganancia_neta


@requires_db
def test_torneo_cancelado_cuenta_sus_inscritos(fabrica):
    jugadores = [fabrica.billetera() for _ in range(2)]
    event_id = _evento(fabrica, TORNEO_CANCELADO, 0)
    for j in jugadores:
        _inscribir(event_id, j)
        _movimiento(j, LEDGER_REFUND, 100, event_id)

    [fila] = _participacion(event_id=event_id)
    assert (fila.tournament_status, fila.inscritos, fila.botin, fila.ganancia_torneo) == (TORNEO_CANCELADO, 2, 0, 0)


@requires_db
def test_filtros_por_modo_y_tipo(fabrica):
    event_id = _sembrar(fabrica)

    [individual] = _participacion(event_id=event_id, modo="individual")
    assert (individual.inscritos, individual.botin, individual.ganancia_torneo) == (0, 0, 0)
    assert (individual.solo_individuales, individual.ganancia_individual) == (2, 90)

    [torneo] = _participacion(event_id=event_id, modo="torneo")
    assert (torneo.solo_individuales, torneo.ganancia_individual, torneo.apostado_pendiente) == (0, 0, 0)
    assert (torneo.inscritos, torneo.ganancia_torneo) == (4, 40)

    [estatica] = _participacion(event_id=event_id, kind="estatica")
    assert (estatica.inscritos, estatica.botin, estatica.ganancia_torneo) == (0, 0, 0)
    assert (estatica.solo_individuales, estatica.ganancia_individual) == (2, 90)
    [dinamica] = _participacion(event_id=event_id, kind="dinamica")
    assert (dinamica.solo_individuales, dinamica.ganancia_individual) == (0, 0)


@requires_db
def test_las_fechas_eligen_eventos_por_su_largada(fabrica):
    event_id = _sembrar(fabrica)
    with SessionLocal() as db:
        largada = db.get(Event, event_id).starts_at

    dentro = _participacion(desde=largada - timedelta(minutes=1), hasta=largada + timedelta(minutes=1))
    assert event_id in [f.event_id for f in dentro]
    # El evento elegido se muestra completo, aunque sus movimientos sean de otra fecha.
    assert next(f for f in dentro if f.event_id == event_id).inscritos == 4
    pasado = datetime(2020, 1, 1, tzinfo=timezone.utc)
    assert event_id not in [f.event_id for f in _participacion(hasta=pasado)]


@requires_db
def test_consulta_por_el_broker(fabrica):
    event_id = _sembrar(fabrica)
    mensaje = {"tipo": "participacion", "filtros": {"event_id": str(event_id)}}

    with SessionLocal() as db:
        r = json.loads(procesar_consulta(db, json.dumps(mensaje).encode()))
        assert (r["tipo"], r["ok"]) == ("participacion", True)
        [fila] = r["datos"]["eventos"]
        assert (fila["event_id"], fila["inscritos"], fila["solo_individuales"]) == (str(event_id), 4, 2)

        malo = json.dumps({"tipo": "participacion", "filtros": {"plantilla": "x"}}).encode()
        assert json.loads(procesar_consulta(db, malo))["error"] == "filtros_invalidos"

        vacio = json.dumps({"tipo": "participacion", "filtros": {"event_id": str(uuid.uuid4())}}).encode()
        assert json.loads(procesar_consulta(db, vacio))["datos"] == {"eventos": []}
