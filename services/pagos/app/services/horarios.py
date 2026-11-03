"""HU2-22 (RF-27): ganancia y volumen apostado por franja horaria y por vuelta.

Sale de las apuestas individuales de bets: son las que mueven tokens. Las de
torneo se pagan en fichas y hoy no se registran en Pagos (las guarda el Motor
en Redis), asi que con modo=torneo el resultado sale vacio.

  - volumen  = suma de amount (incluye las apuestas aun sin resolver);
  - ganancia = suma de amount - payout, con payout nulo como 0.
Es la misma definicion que el resumen financiero (HU2-20): debit - settle.

  - por_hora: franjas de 1 hora (0-23) en hora de Colombia; las fechas se
    guardan en UTC.
  - por_vuelta: por bets.lap. vuelta=None agrupa las apuestas sin vuelta
    (antes de la largada, o si quien apuesta no la informa).

Filtros (RF-29): event_id, desde/hasta (fecha de la apuesta), kind (por el
mercado de la apuesta) y modo.
"""
from dataclasses import dataclass

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.models import BET_INDIVIDUAL, Bet, Market
from app.services.resumen_financiero import MODO_TORNEO, FiltrosResumen

ZONA_HORARIA = "America/Bogota"


@dataclass(frozen=True)
class Franja:
    clave: int | None  # hora (0-23) o vuelta; None = apuestas sin vuelta
    volumen: int
    ganancia: int


@dataclass(frozen=True)
class Horarios:
    por_hora: list[Franja]
    por_vuelta: list[Franja]


def _agrupar(db: Session, columna, f: FiltrosResumen) -> list[Franja]:
    stmt = (
        select(
            columna,
            func.sum(Bet.amount),
            func.sum(Bet.amount - func.coalesce(Bet.payout, 0)),
        )
        .where(Bet.mode == BET_INDIVIDUAL)
        .group_by(columna)
    )
    if f.event_id is not None:
        stmt = stmt.where(Bet.event_id == f.event_id)
    if f.desde is not None:
        stmt = stmt.where(Bet.created_at >= f.desde)
    if f.hasta is not None:
        stmt = stmt.where(Bet.created_at <= f.hasta)
    if f.kind is not None:
        stmt = stmt.join(Market, Bet.market_id == Market.id).where(Market.kind == f.kind)
    filas = [
        Franja(None if clave is None else int(clave), int(volumen), int(ganancia))
        for clave, volumen, ganancia in db.execute(stmt).all()
    ]
    return sorted(filas, key=lambda x: (x.clave is not None, x.clave or 0))


def calcular_horarios(db: Session, filtros: FiltrosResumen) -> Horarios:
    if filtros.modo == MODO_TORNEO:
        return Horarios(por_hora=[], por_vuelta=[])
    hora = func.extract("hour", func.timezone(ZONA_HORARIA, Bet.created_at))
    return Horarios(
        por_hora=_agrupar(db, hora, filtros),
        por_vuelta=_agrupar(db, Bet.lap, filtros),
    )
