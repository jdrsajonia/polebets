"""HU2-20 (RF-25): resumen financiero, con los filtros de RF-29.

Se siembran movimientos del ledger con totales conocidos, como los dejan
HU2-04 (compra), HU2-06 (inscripcion y devolucion), HU2-10 (debito y
liquidacion) y HU2-12 (premios).
"""
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
    BET_WON,
    LEDGER_BUY_IN,
    LEDGER_DEBIT,
    LEDGER_PRIZE,
    LEDGER_PURCHASE,
    LEDGER_REFUND,
    LEDGER_SETTLE,
    MARKET_DINAMICA,
    MARKET_ESTATICA,
    TORNEO_ACTIVO,
    TORNEO_CANCELADO,
    TORNEO_REPARTIDO,
    Bet,
    Event,
    LedgerEntry,
    Market,
)
from app.services.billetera import tasa
from app.services.consulta_handler import procesar_consulta
from app.services.resumen_financiero import FiltrosResumen, calcular_resumen
from tests.conftest import requires_db


def _resumen(**filtros):
    with SessionLocal() as db:
        return calcular_resumen(db, FiltrosResumen(**filtros))


def _movimiento(user_id, tipo, tokens, event_id=None, bet_id=None, creado=None):
    with SessionLocal() as db:
        m = LedgerEntry(
            user_id=user_id,
            event_id=event_id,
            bet_id=bet_id,
            type=tipo,
            tokens_delta=tokens,
            idempotency_key=f"prueba:{uuid.uuid4()}",
        )
        if creado is not None:
            m.created_at = creado
        db.add(m)
        db.commit()


def _evento(fabrica, tournament_status, pot):
    evento = fabrica.evento()
    with SessionLocal() as db:
        db.execute(update(Event).where(Event.id == evento.id).values(tournament_status=tournament_status, pot=pot))
        db.commit()
    return evento.id


def _mercado(event_id, kind):
    with SessionLocal() as db:
        m = Market(event_id=event_id, kind=kind, template="ganador", question="¿?", options=[{"opcion": "VER", "multiplicador": 2}])
        db.add(m)
        db.commit()
        return m.id


def _apuesta(user_id, event_id, market_id, amount, status, payout=None):
    """Apuesta individual con su debito (y su liquidacion, si ya se resolvio)."""
    bet_id = uuid.uuid4()
    with SessionLocal() as db:
        db.add(
            Bet(
                id=bet_id,
                user_id=user_id,
                market_id=market_id,
                event_id=event_id,
                mode=BET_INDIVIDUAL,
                option="VER",
                amount=amount,
                multiplier=Decimal("2.5"),
                status=status,
                payout=payout,
            )
        )
        db.commit()
    _movimiento(user_id, LEDGER_DEBIT, -amount, event_id, bet_id)
    if status != BET_PENDING:
        _movimiento(user_id, LEDGER_SETTLE, payout, event_id, bet_id)


def _sembrar(fabrica):
    """Un evento ya repartido, con apuestas individuales y una compra sin evento.

    Torneo: 4 inscritos x 100 = botin 400; comision 10 % = 40; premios 360.
    Individual: apostado 100 + 40 + 30 = 170; pagado 250 + 0 = 250.
    """
    jugadores = [fabrica.billetera() for _ in range(4)]
    evento = _evento(fabrica, TORNEO_REPARTIDO, 400)
    estatica, dinamica = _mercado(evento, MARKET_ESTATICA), _mercado(evento, MARKET_DINAMICA)

    _movimiento(jugadores[0], LEDGER_PURCHASE, 500)
    _apuesta(jugadores[0], evento, estatica, 100, BET_WON, 250)
    _apuesta(jugadores[1], evento, dinamica, 40, BET_LOST, 0)
    _apuesta(jugadores[2], evento, estatica, 30, BET_PENDING)
    for j in jugadores:
        _movimiento(j, LEDGER_BUY_IN, -100, evento)
    _movimiento(jugadores[0], LEDGER_PRIZE, 216, evento)
    _movimiento(jugadores[1], LEDGER_PRIZE, 144, evento)
    return evento


@requires_db
def test_resumen_de_un_evento_repartido(fabrica):
    evento = _sembrar(fabrica)

    r = _resumen(event_id=evento)

    assert r.tokens_comprados == 0  # la compra no pertenece a ningun evento
    assert (r.apostado_individual, r.pagado_individual) == (170, 250)
    assert (r.inscripciones_cobradas, r.inscripciones_devueltas, r.premios_pagados) == (400, 0, 360)
    # Individual: 170 - 250 = -80. Torneo: 400 - 360 = 40, exactamente la comision.
    assert r.ganancia_neta == -40
    assert r.botin_en_juego == 0  # repartido: ya no esta en juego
    assert r.apostado_pendiente == 30


@requires_db
def test_sin_filtros_incluye_las_compras_y_su_equivalente_en_cop(fabrica):
    antes = _resumen()
    _sembrar(fabrica)
    despues = _resumen()

    assert despues.tokens_comprados - antes.tokens_comprados == 500
    assert despues.cop_comprados - antes.cop_comprados == 500 * tasa()
    assert despues.ganancia_neta - antes.ganancia_neta == -40


@requires_db
def test_filtro_por_modo(fabrica):
    evento = _sembrar(fabrica)

    individual = _resumen(event_id=evento, modo="individual")
    assert (individual.apostado_individual, individual.pagado_individual, individual.apostado_pendiente) == (170, 250, 30)
    assert (individual.inscripciones_cobradas, individual.premios_pagados) == (0, 0)
    assert individual.ganancia_neta == -80

    torneo = _resumen(event_id=evento, modo="torneo")
    assert (torneo.apostado_individual, torneo.pagado_individual, torneo.apostado_pendiente) == (0, 0, 0)
    assert (torneo.inscripciones_cobradas, torneo.premios_pagados) == (400, 360)
    assert torneo.ganancia_neta == 40


@requires_db
def test_filtro_por_tipo_de_apuesta(fabrica):
    evento = _sembrar(fabrica)

    estatica = _resumen(event_id=evento, kind="estatica")
    assert (estatica.apostado_individual, estatica.pagado_individual, estatica.apostado_pendiente) == (130, 250, 30)
    assert (estatica.inscripciones_cobradas, estatica.premios_pagados, estatica.botin_en_juego) == (0, 0, 0)
    assert estatica.ganancia_neta == -120

    dinamica = _resumen(event_id=evento, kind="dinamica")
    assert (dinamica.apostado_individual, dinamica.pagado_individual, dinamica.apostado_pendiente) == (40, 0, 0)
    assert dinamica.ganancia_neta == 40


@requires_db
def test_con_modo_o_tipo_las_compras_salen_en_cero(fabrica):
    antes = _resumen(modo="individual")
    _sembrar(fabrica)
    assert _resumen(modo="individual").tokens_comprados == antes.tokens_comprados == 0
    assert _resumen(kind="estatica").tokens_comprados == 0


@requires_db
def test_torneo_cancelado_devuelve_todo_y_no_deja_ganancia(fabrica):
    jugadores = [fabrica.billetera() for _ in range(2)]
    evento = _evento(fabrica, TORNEO_CANCELADO, 0)  # HU2-06 deja el botin en 0
    for j in jugadores:
        _movimiento(j, LEDGER_BUY_IN, -100, evento)
        _movimiento(j, LEDGER_REFUND, 100, evento)

    r = _resumen(event_id=evento)
    assert (r.inscripciones_cobradas, r.inscripciones_devueltas, r.ganancia_neta, r.botin_en_juego) == (200, 200, 0, 0)


@requires_db
def test_botin_en_juego_solo_de_torneos_abiertos_o_activos(fabrica):
    activo = _evento(fabrica, TORNEO_ACTIVO, 300)
    repartido = _evento(fabrica, TORNEO_REPARTIDO, 400)

    assert _resumen(event_id=activo).botin_en_juego == 300
    assert _resumen(event_id=repartido).botin_en_juego == 0
    assert _resumen(event_id=activo, modo="individual").botin_en_juego == 0
    # Foto del momento: las fechas no lo filtran.
    pasado = datetime(2020, 1, 1, tzinfo=timezone.utc)
    assert _resumen(event_id=activo, hasta=pasado).botin_en_juego == 300


@requires_db
def test_filtro_por_fechas(fabrica):
    jugador = fabrica.billetera()
    evento = _evento(fabrica, TORNEO_ACTIVO, 200)
    dia = datetime(2026, 10, 15, 12, tzinfo=timezone.utc)
    _movimiento(jugador, LEDGER_BUY_IN, -100, evento, creado=dia - timedelta(days=2))
    _movimiento(jugador, LEDGER_BUY_IN, -100, evento, creado=dia)

    r = _resumen(event_id=evento, desde=dia - timedelta(hours=1), hasta=dia + timedelta(hours=1))
    assert r.inscripciones_cobradas == 100
    assert _resumen(event_id=evento).inscripciones_cobradas == 200


@requires_db
def test_consulta_por_el_broker(fabrica):
    evento = _sembrar(fabrica)
    mensaje = {"tipo": "resumen_financiero", "filtros": {"event_id": str(evento), "modo": "torneo"}}

    with SessionLocal() as db:
        r = json.loads(procesar_consulta(db, json.dumps(mensaje).encode()))
        assert r["tipo"] == "resumen_financiero" and r["ok"] is True
        assert r["datos"]["ganancia_neta"] == 40
        assert set(r["datos"]) == {
            "tokens_comprados", "cop_comprados", "apostado_individual", "pagado_individual",
            "inscripciones_cobradas", "inscripciones_devueltas", "premios_pagados",
            "ganancia_neta", "botin_en_juego", "apostado_pendiente",
        }

        for filtros in ({"template": "ganador"}, {"top_n": 5}, {"modo": "mixto"}, {"kind": "otra"}):
            malo = json.dumps({"tipo": "resumen_financiero", "filtros": filtros}).encode()
            assert json.loads(procesar_consulta(db, malo))["error"] == "filtros_invalidos", filtros
