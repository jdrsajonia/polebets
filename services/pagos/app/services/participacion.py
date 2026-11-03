"""HU2-22 (RF-28): participacion por modo, una fila por evento.

  - inscritos: filas de event_entries. Incluye las de un torneo cancelado, como
    el calendario (documentation/orquestador-api.md).
  - solo_individuales: jugadores con apuestas individuales en el evento que no
    estan inscritos.
  - botin: events.pot (0 si el torneo se cancelo: HU2-06 lo devuelve).
  - ganancia_torneo, ganancia_individual, apostado_pendiente: del resumen
    financiero (HU2-20) del evento, por modo, para que las dos consultas cuadren.
    Mientras el torneo no este repartido, su ganancia incluye el botin; por
    eso cada fila lleva tournament_status.

Filtros (RF-29). Cada dato pertenece a un modo, como en el resumen:
  - event_id: solo ese evento.
  - desde/hasta: eligen los eventos por su largada (starts_at); cada evento se
    muestra completo, porque RF-28 es por evento.
  - modo: deja solo los datos de ese modo; los del otro salen en 0.
  - kind: aplica a la parte individual; la del torneo sale en 0 (el torneo no
    es de un tipo de apuesta).
"""
import uuid
from dataclasses import dataclass, replace
from datetime import datetime

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.models import BET_INDIVIDUAL, Bet, Event, EventEntry, Market
from app.services.resumen_financiero import MODO_INDIVIDUAL, MODO_TORNEO, FiltrosResumen, calcular_resumen


@dataclass(frozen=True)
class FilaEvento:
    event_id: uuid.UUID
    race_key: str
    starts_at: datetime
    tournament_status: str
    inscritos: int
    solo_individuales: int
    botin: int
    ganancia_torneo: int
    ganancia_individual: int
    apostado_pendiente: int


def _inscritos(db: Session, event_id: uuid.UUID) -> int:
    return db.scalar(select(func.count()).select_from(EventEntry).where(EventEntry.event_id == event_id))


def _solo_individuales(db: Session, event_id: uuid.UUID, kind: str | None) -> int:
    inscritos = select(EventEntry.user_id).where(EventEntry.event_id == event_id)
    stmt = select(func.count(func.distinct(Bet.user_id))).where(
        Bet.event_id == event_id,
        Bet.mode == BET_INDIVIDUAL,
        Bet.user_id.not_in(inscritos),
    )
    if kind is not None:
        stmt = stmt.join(Market, Bet.market_id == Market.id).where(Market.kind == kind)
    return db.scalar(stmt)


def calcular_participacion(db: Session, filtros: FiltrosResumen) -> list[FilaEvento]:
    stmt = select(Event).order_by(Event.starts_at.desc())
    if filtros.event_id is not None:
        stmt = stmt.where(Event.id == filtros.event_id)
    if filtros.desde is not None:
        stmt = stmt.where(Event.starts_at >= filtros.desde)
    if filtros.hasta is not None:
        stmt = stmt.where(Event.starts_at <= filtros.hasta)

    torneo = filtros.modo != MODO_INDIVIDUAL and filtros.kind is None
    individual = filtros.modo != MODO_TORNEO
    filas = []
    for ev in db.scalars(stmt):
        # Sin fechas: el evento elegido se muestra completo.
        por_evento = replace(filtros, event_id=ev.id, desde=None, hasta=None)
        r_torneo = calcular_resumen(db, replace(por_evento, modo=MODO_TORNEO)) if torneo else None
        r_indiv = calcular_resumen(db, replace(por_evento, modo=MODO_INDIVIDUAL)) if individual else None
        filas.append(
            FilaEvento(
                event_id=ev.id,
                race_key=ev.race_key,
                starts_at=ev.starts_at,
                tournament_status=ev.tournament_status,
                inscritos=_inscritos(db, ev.id) if torneo else 0,
                solo_individuales=_solo_individuales(db, ev.id, filtros.kind) if individual else 0,
                botin=ev.pot if torneo else 0,
                ganancia_torneo=r_torneo.ganancia_neta if r_torneo else 0,
                ganancia_individual=r_indiv.ganancia_neta if r_indiv else 0,
                apostado_pendiente=r_indiv.apostado_pendiente if r_indiv else 0,
            )
        )
    return filas
