"""Mercados para el Motor (HU2-09 y HU2-10).

  GET  /internal/markets?status=cerrado&resuelto=true  mercados listos para liquidar (HU2-10)
  GET  /internal/markets/{market_id}                   estado, opciones y multiplicadores (HU2-09)
  GET  /internal/markets/{market_id}/bets              apuestas del mercado (HU2-10)
  POST /internal/markets/{market_id}/liquidated        marcar el mercado como liquidado (HU2-10)

Los mercados los crean HU2-07 (estaticas) y HU2-08 (dinamicas) y quien conoce
el resultado de la carrera escribe winning_option. Aqui no se decide ningun
resultado: el Motor lee, paga cada apuesta con POST /internal/settlements y al
final marca el mercado.
"""
import uuid
from typing import Literal

from fastapi import APIRouter, HTTPException, Query, Response, status
from sqlalchemy import exists, select

from app.deps import DbSession, ServicioInterno
from app.models import BET_INDIVIDUAL, BET_PENDING, MARKET_CERRADO, MARKET_LIQUIDADO, Bet, Market
from app.schemas.markets import MarketBetOut, MarketLiquidatedOut, MarketOut, OpcionOut

router = APIRouter(prefix="/internal/markets", tags=["mercados"], dependencies=[ServicioInterno])

NO_EXISTE = "Mercado inexistente"


def _salida(mercado: Market) -> MarketOut:
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


def _pendientes_individuales(market_id: uuid.UUID):
    return exists().where(Bet.market_id == market_id, Bet.mode == BET_INDIVIDUAL, Bet.status == BET_PENDING)


@router.get("", response_model=list[MarketOut])
def listar(
    db: DbSession,
    response: Response,
    estado: Literal["programado", "abierto", "cerrado", "liquidado"] | None = Query(None, alias="status"),
    resuelto: bool | None = Query(None, description="true: solo con winning_option; false: solo sin ella"),
    limit: int = Query(50, ge=1, le=200),
) -> list[MarketOut]:
    consulta = select(Market)
    if estado is not None:
        consulta = consulta.where(Market.status == estado)
    if resuelto is True:
        consulta = consulta.where(Market.winning_option.is_not(None))
    elif resuelto is False:
        consulta = consulta.where(Market.winning_option.is_(None))
    consulta = consulta.order_by(Market.closes_at.asc().nulls_last(), Market.id).limit(limit)
    response.headers["Cache-Control"] = "no-store"
    return [_salida(m) for m in db.scalars(consulta)]


@router.get("/{market_id}", response_model=MarketOut, responses={404: {"description": NO_EXISTE}})
def consultar(market_id: uuid.UUID, db: DbSession, response: Response) -> MarketOut:
    mercado = db.get(Market, market_id)
    if mercado is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=NO_EXISTE)
    response.headers["Cache-Control"] = "no-store"  # el estado cambia en vivo
    return _salida(mercado)


@router.get("/{market_id}/bets", response_model=list[MarketBetOut], responses={404: {"description": NO_EXISTE}})
def apuestas(
    market_id: uuid.UUID,
    db: DbSession,
    response: Response,
    modo: Literal["individual", "torneo"] | None = Query(None, alias="mode"),
    estado: Literal["pending", "won", "lost"] | None = Query(None, alias="status"),
) -> list[MarketBetOut]:
    if db.get(Market, market_id) is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=NO_EXISTE)
    consulta = select(Bet).where(Bet.market_id == market_id)
    if modo is not None:
        consulta = consulta.where(Bet.mode == modo)
    if estado is not None:
        consulta = consulta.where(Bet.status == estado)
    response.headers["Cache-Control"] = "no-store"
    return [
        MarketBetOut(
            bet_id=b.id,
            user_id=b.user_id,
            mode=b.mode,
            option=b.option,
            amount=b.amount,
            multiplier=float(b.multiplier),
            status=b.status,
            payout=b.payout,
        )
        for b in db.scalars(consulta.order_by(Bet.created_at, Bet.id))
    ]


@router.post(
    "/{market_id}/liquidated",
    response_model=MarketLiquidatedOut,
    responses={
        404: {"description": NO_EXISTE},
        409: {"description": "El mercado no esta cerrado con ganador o aun tiene apuestas individuales pendientes"},
    },
)
def marcar_liquidado(market_id: uuid.UUID, db: DbSession) -> MarketLiquidatedOut:
    """Pasa el mercado de cerrado a liquidado. Repetirlo no cambia nada (RNF-12)."""
    mercado = db.scalar(select(Market).where(Market.id == market_id).with_for_update())
    if mercado is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=NO_EXISTE)
    if mercado.status == MARKET_LIQUIDADO:
        return MarketLiquidatedOut(market_id=mercado.id, status=mercado.status, nueva=False)
    if mercado.status != MARKET_CERRADO or mercado.winning_option is None:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="El mercado no está cerrado con ganador")
    if db.scalar(select(_pendientes_individuales(market_id))):
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Quedan apuestas individuales pendientes")
    mercado.status = MARKET_LIQUIDADO
    db.commit()
    return MarketLiquidatedOut(market_id=mercado.id, status=mercado.status, nueva=True)