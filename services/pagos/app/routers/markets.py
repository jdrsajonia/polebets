"""HU2-09: lectura de mercados para el Motor.

  GET /internal/markets/{market_id}   estado, opciones y multiplicadores

Los mercados los crean HU2-07 (estaticas) y HU2-08 (dinamicas); aqui solo se
leen. El Motor los consulta al apostar para validar que el mercado este
abierto, que la opcion exista y tomar su multiplicador vigente.
"""
import uuid

from fastapi import APIRouter, HTTPException, Response, status

from app.deps import DbSession, ServicioInterno
from app.models import Market
from app.schemas.markets import MarketOut, OpcionOut

router = APIRouter(prefix="/internal/markets", tags=["mercados"], dependencies=[ServicioInterno])


@router.get("/{market_id}", response_model=MarketOut, responses={404: {"description": "Mercado inexistente"}})
def consultar(market_id: uuid.UUID, db: DbSession, response: Response) -> MarketOut:
    mercado = db.get(Market, market_id)
    if mercado is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Mercado inexistente")
    response.headers["Cache-Control"] = "no-store"  # el estado cambia en vivo
    return MarketOut(
        market_id=mercado.id,
        event_id=mercado.event_id,
        kind=mercado.kind,
        template=mercado.template,
        question=mercado.question,
        options=[OpcionOut(opcion=o["opcion"], multiplicador=float(o["multiplicador"])) for o in mercado.options],
        status=mercado.status,
        opens_at=mercado.opens_at,
        closes_at=mercado.closes_at,
        race_close_s=mercado.race_close_s,
        winning_option=mercado.winning_option,
    )
