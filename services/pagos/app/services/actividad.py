"""Lecturas de la actividad de un jugador para la pagina SSR (HU2-03).

Solo lee: la pagina del usuario no cambia nada. Cada funcion responde una
seccion de la pagina y el Orquestador las pide en paralelo.
"""
import uuid
from collections.abc import Sequence

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import Bet, Event, EventEntry, LedgerEntry


def eventos_inscritos(db: Session, user_id: uuid.UUID) -> Sequence[tuple[EventEntry, Event]]:
    """Inscripciones del jugador, la largada mas reciente primero (incluye las devueltas)."""
    return db.execute(
        select(EventEntry, Event)
        .join(Event, Event.id == EventEntry.event_id)
        .where(EventEntry.user_id == user_id)
        .order_by(Event.starts_at.desc())
    ).all()


def apuestas(db: Session, user_id: uuid.UUID, estado: str | None, limite: int) -> Sequence[Bet]:
    """Apuestas del jugador, la mas reciente primero; `estado` filtra (p. ej. pending)."""
    consulta = select(Bet).where(Bet.user_id == user_id)
    if estado is not None:
        consulta = consulta.where(Bet.status == estado)
    return db.scalars(consulta.order_by(Bet.created_at.desc()).limit(limite)).all()


def movimientos(db: Session, user_id: uuid.UUID, limite: int) -> Sequence[LedgerEntry]:
    """Ultimos movimientos de tokens del ledger, el mas reciente primero."""
    return db.scalars(
        select(LedgerEntry)
        .where(LedgerEntry.user_id == user_id)
        .order_by(LedgerEntry.created_at.desc(), LedgerEntry.id.desc())
        .limit(limite)
    ).all()
