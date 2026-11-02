"""HU2-03 (parte Pagos): actividad de un jugador para la pagina SSR.

Solo para servicios (header X-Internal-Service-Token). Las llama el
Orquestador en GET /pagina-usuario (documentation/orquestador-api.md, §3.2):

  GET /internal/users/{user_id}/event-entries          eventos donde esta inscrito
  GET /internal/users/{user_id}/bets?status=pending    apuestas (filtro opcional por estado)
  GET /internal/users/{user_id}/ledger?limit=10        ultimos movimientos de tokens

Un jugador sin actividad recibe listas vacias (no 404): la pagina igual se arma.
"""
import uuid
from typing import Annotated, Literal

from fastapi import APIRouter, Query, Response

from app.deps import DbSession, ServicioInterno
from app.schemas.actividad import BetOut, EventEntryOut, LedgerOut
from app.services.actividad import apuestas, eventos_inscritos, movimientos

router = APIRouter(prefix="/internal/users", tags=["actividad"], dependencies=[ServicioInterno])


def _sin_cache(response: Response) -> None:
    # Saldos y apuestas cambian a cada rato: que ningun proxy lo guarde.
    response.headers["Cache-Control"] = "no-store"


@router.get("/{user_id}/event-entries", response_model=list[EventEntryOut])
def ver_inscripciones(user_id: uuid.UUID, db: DbSession, response: Response) -> list[EventEntryOut]:
    _sin_cache(response)
    return [
        EventEntryOut(
            event_id=evento.id,
            race_key=evento.race_key,
            status=evento.status,
            tournament_status=evento.tournament_status,
            starts_at=evento.starts_at,
            chips_final=entrada.chips_final,
            position_final=entrada.position_final,
            prize=entrada.prize,
        )
        for entrada, evento in eventos_inscritos(db, user_id)
    ]


@router.get("/{user_id}/bets", response_model=list[BetOut])
def ver_apuestas(
    user_id: uuid.UUID,
    db: DbSession,
    response: Response,
    status: Annotated[Literal["pending", "won", "lost"] | None, Query()] = None,
    limit: Annotated[int, Query(ge=1, le=100)] = 50,
) -> list[BetOut]:
    _sin_cache(response)
    return [
        BetOut(
            bet_id=b.id,
            market_id=b.market_id,
            event_id=b.event_id,
            option=b.option,
            amount=b.amount,
            mode=b.mode,
            multiplier=float(b.multiplier),
            status=b.status,
            created_at=b.created_at,
        )
        for b in apuestas(db, user_id, status, limit)
    ]


@router.get("/{user_id}/ledger", response_model=list[LedgerOut])
def ver_movimientos(
    user_id: uuid.UUID,
    db: DbSession,
    response: Response,
    limit: Annotated[int, Query(ge=1, le=50)] = 10,
) -> list[LedgerOut]:
    _sin_cache(response)
    return [
        LedgerOut(type=m.type, tokens_delta=m.tokens_delta, event_id=m.event_id, created_at=m.created_at)
        for m in movimientos(db, user_id, limit)
    ]
