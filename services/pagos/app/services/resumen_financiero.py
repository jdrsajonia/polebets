"""HU2-20 (RF-25): resumen financiero de la plataforma.

Cuanto entra y cuanto sale, por evento o por periodo. Todo sale del ledger
(RNF-20: cada movimiento de tokens queda ahi), sumando tokens_delta por tipo:

    purchase  compra de tokens          (+)  -> tokens comprados
    debit     apuesta individual        (-)  -> apostado en individual
    settle    liquidacion individual    (+)  -> pagado en individual
    buy_in    inscripcion al torneo     (-)  -> inscripciones cobradas
    refund    torneo cancelado          (+)  -> inscripciones devueltas
    prize     premio del reparto        (+)  -> premios pagados

ganancia_neta = apostado - pagado + inscripciones - devueltas - premios. En un
evento ya repartido es exactamente comision + residuo; en uno que sigue en
curso todavia incluye el botin y las apuestas sin resolver, por eso se informa
aparte lo que sigue en juego (botin_en_juego, apostado_pendiente).

Filtros (RF-29). Cada dato pertenece a un modo: las compras a ninguno, las
apuestas individuales al individual y las inscripciones, premios y botin al
torneo.
  - event_id, desde, hasta: a todo; el botin en juego es una foto del momento
    y no se filtra por fechas.
  - modo: deja solo los datos de ese modo.
  - kind: deja solo las apuestas individuales de ese tipo (el torneo no es de
    un tipo).
Con modo o kind, las compras salen en 0: no son apuestas.

La consulta pura: no sabe nada de RabbitMQ (ver consulta_handler.py).
"""
import uuid
from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.models import (
    BET_INDIVIDUAL,
    BET_PENDING,
    LEDGER_BUY_IN,
    LEDGER_DEBIT,
    LEDGER_PRIZE,
    LEDGER_PURCHASE,
    LEDGER_REFUND,
    LEDGER_SETTLE,
    TORNEO_ABIERTO,
    TORNEO_ACTIVO,
    Bet,
    Event,
    LedgerEntry,
    Market,
)
from app.services.billetera import tasa

MODO_INDIVIDUAL = "individual"
MODO_TORNEO = "torneo"

_TIPOS_POR_MODO = {
    MODO_INDIVIDUAL: (LEDGER_DEBIT, LEDGER_SETTLE),
    MODO_TORNEO: (LEDGER_BUY_IN, LEDGER_REFUND, LEDGER_PRIZE),
}


@dataclass(frozen=True)
class FiltrosResumen:
    """Filtros de RF-29. Todos opcionales; None = sin filtrar por ese campo."""

    event_id: uuid.UUID | None = None
    desde: datetime | None = None  # created_at del movimiento >= desde
    hasta: datetime | None = None  # created_at del movimiento <= hasta
    kind: str | None = None  # "estatica" | "dinamica"
    modo: str | None = None  # "individual" | "torneo"


@dataclass(frozen=True)
class ResumenFinanciero:
    tokens_comprados: int
    cop_comprados: int
    apostado_individual: int  # incluye las apuestas aun sin resolver
    pagado_individual: int
    inscripciones_cobradas: int
    inscripciones_devueltas: int
    premios_pagados: int
    ganancia_neta: int
    botin_en_juego: int  # torneos abiertos o activos
    apostado_pendiente: int  # apuestas individuales sin resolver


def _sumas_del_ledger(db: Session, f: FiltrosResumen) -> dict[str, int]:
    """Suma de tokens_delta por tipo de movimiento, con los filtros aplicados."""
    stmt = select(LedgerEntry.type, func.sum(LedgerEntry.tokens_delta)).group_by(LedgerEntry.type)
    if f.event_id is not None:
        stmt = stmt.where(LedgerEntry.event_id == f.event_id)
    if f.desde is not None:
        stmt = stmt.where(LedgerEntry.created_at >= f.desde)
    if f.hasta is not None:
        stmt = stmt.where(LedgerEntry.created_at <= f.hasta)
    if f.modo is not None:
        stmt = stmt.where(LedgerEntry.type.in_(_TIPOS_POR_MODO[f.modo]))
    if f.kind is not None:
        # Solo los movimientos de una apuesta tienen tipo (por su mercado).
        stmt = (
            stmt.join(Bet, LedgerEntry.bet_id == Bet.id)
            .join(Market, Bet.market_id == Market.id)
            .where(Market.kind == f.kind)
        )
    return {tipo: int(total) for tipo, total in db.execute(stmt).all()}


def _botin_en_juego(db: Session, f: FiltrosResumen) -> int:
    if f.kind is not None or f.modo == MODO_INDIVIDUAL:
        return 0
    # Al cancelar, HU2-06 deja el botin en 0; al repartir, HU2-12 no lo toca:
    # por eso solo cuentan los torneos que siguen abiertos o activos.
    stmt = select(func.coalesce(func.sum(Event.pot), 0)).where(
        Event.tournament_status.in_((TORNEO_ABIERTO, TORNEO_ACTIVO))
    )
    if f.event_id is not None:
        stmt = stmt.where(Event.id == f.event_id)
    return int(db.scalar(stmt))


def _apostado_pendiente(db: Session, f: FiltrosResumen) -> int:
    if f.modo == MODO_TORNEO:
        return 0
    stmt = select(func.coalesce(func.sum(Bet.amount), 0)).where(
        Bet.mode == BET_INDIVIDUAL, Bet.status == BET_PENDING
    )
    if f.event_id is not None:
        stmt = stmt.where(Bet.event_id == f.event_id)
    if f.desde is not None:
        stmt = stmt.where(Bet.created_at >= f.desde)
    if f.hasta is not None:
        stmt = stmt.where(Bet.created_at <= f.hasta)
    if f.kind is not None:
        stmt = stmt.join(Market, Bet.market_id == Market.id).where(Market.kind == f.kind)
    return int(db.scalar(stmt))


def calcular_resumen(db: Session, filtros: FiltrosResumen) -> ResumenFinanciero:
    sumas = _sumas_del_ledger(db, filtros)
    comprados = sumas.get(LEDGER_PURCHASE, 0)
    apostado = -sumas.get(LEDGER_DEBIT, 0)
    pagado = sumas.get(LEDGER_SETTLE, 0)
    cobradas = -sumas.get(LEDGER_BUY_IN, 0)
    devueltas = sumas.get(LEDGER_REFUND, 0)
    premios = sumas.get(LEDGER_PRIZE, 0)
    return ResumenFinanciero(
        tokens_comprados=comprados,
        cop_comprados=comprados * tasa(),
        apostado_individual=apostado,
        pagado_individual=pagado,
        inscripciones_cobradas=cobradas,
        inscripciones_devueltas=devueltas,
        premios_pagados=premios,
        ganancia_neta=apostado - pagado + cobradas - devueltas - premios,
        botin_en_juego=_botin_en_juego(db, filtros),
        apostado_pendiente=_apostado_pendiente(db, filtros),
    )
