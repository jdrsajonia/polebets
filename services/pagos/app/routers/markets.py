"""Mercados para el Motor (HU2-07, HU2-08, HU2-09 y HU2-10).

  POST /internal/markets                               crear un mercado (HU2-07 estaticas, HU2-08 dinamicas)
  POST /internal/markets/{market_id}/open              programado -> abierto
  POST /internal/markets/{market_id}/close             programado|abierto -> cerrado
  POST /internal/markets/{market_id}/resolve           guardar winning_option de un mercado cerrado
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
from sqlalchemy.exc import IntegrityError

from app.deps import DbSession, ServicioInterno
from app.models import (
    BET_INDIVIDUAL,
    BET_PENDING,
    MARKET_ABIERTO,
    MARKET_CERRADO,
    MARKET_LIQUIDADO,
    MARKET_PROGRAMADO,
    Bet,
    Event,
    Market,
)
from app.schemas.markets import (
    MarketBetOut,
    MarketCreate,
    MarketLiquidatedOut,
    MarketOut,
    MarketResolve,
    OpcionOut,
)

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


def _opciones(datos: MarketCreate) -> list[dict]:
    return [{"opcion": o.opcion, "multiplicador": o.multiplicador} for o in datos.options]


def _normalizadas(opciones: list[dict]) -> list[tuple[str, float]]:
    return [(o["opcion"], float(o["multiplicador"])) for o in opciones]


def _mismos_datos(mercado: Market, datos: MarketCreate) -> bool:
    """Compara lo que define al mercado. El status no cuenta: puede haber avanzado por open/close."""
    return (
        mercado.event_id == datos.event_id
        and mercado.kind == datos.kind
        and mercado.template == datos.template
        and mercado.question == datos.question
        and _normalizadas(mercado.options) == _normalizadas(_opciones(datos))
        and mercado.opens_at == datos.opens_at
        and mercado.closes_at == datos.closes_at
        and mercado.race_close_s == datos.race_close_s
    )


def _bloquear(db: DbSession, market_id: uuid.UUID) -> Market:
    mercado = db.scalar(select(Market).where(Market.id == market_id).with_for_update())
    if mercado is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=NO_EXISTE)
    return mercado


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


@router.post(
    "",
    response_model=MarketOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Reintento: el mercado ya existia con los mismos datos"},
        404: {"description": "Evento inexistente"},
        409: {"description": "Ya existe un mercado con ese market_id y otros datos"},
    },
)
def crear(datos: MarketCreate, db: DbSession, response: Response) -> MarketOut:
    """Crea un mercado. market_id es la clave de idempotencia: repetirlo con los mismos datos no duplica."""
    response.headers["Cache-Control"] = "no-store"

    def existente() -> MarketOut:
        previo = db.get(Market, datos.market_id)
        if not _mismos_datos(previo, datos):
            raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="El mercado ya existe con otros datos")
        response.status_code = status.HTTP_200_OK
        return _salida(previo)

    if db.get(Market, datos.market_id) is not None:
        return existente()
    if db.get(Event, datos.event_id) is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Evento inexistente")
    mercado = Market(
        id=datos.market_id,
        event_id=datos.event_id,
        kind=datos.kind,
        template=datos.template,
        question=datos.question,
        options=_opciones(datos),
        status=datos.status,
        opens_at=datos.opens_at,
        closes_at=datos.closes_at,
        race_close_s=datos.race_close_s,
    )
    db.add(mercado)
    try:
        db.commit()
    except IntegrityError:
        # Otro POST con el mismo id gano la carrera: se lee lo que quedo y se aplica la misma regla.
        db.rollback()
        db.expire_all()
        return existente()
    return _salida(mercado)


@router.post(
    "/{market_id}/open",
    response_model=MarketOut,
    responses={404: {"description": NO_EXISTE}, 409: {"description": "El mercado ya esta cerrado o liquidado"}},
)
def abrir(market_id: uuid.UUID, db: DbSession, response: Response) -> MarketOut:
    """Pasa el mercado de programado a abierto. Si ya estaba abierto no cambia nada."""
    mercado = _bloquear(db, market_id)
    response.headers["Cache-Control"] = "no-store"
    if mercado.status == MARKET_PROGRAMADO:
        mercado.status = MARKET_ABIERTO
        db.commit()
    elif mercado.status != MARKET_ABIERTO:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="El mercado ya está cerrado")
    return _salida(mercado)


@router.post("/{market_id}/close", response_model=MarketOut, responses={404: {"description": NO_EXISTE}})
def cerrar(market_id: uuid.UUID, db: DbSession, response: Response) -> MarketOut:
    """Pasa el mercado a cerrado (ya no recibe apuestas). Si ya estaba cerrado o liquidado no cambia nada."""
    mercado = _bloquear(db, market_id)
    response.headers["Cache-Control"] = "no-store"
    if mercado.status in (MARKET_PROGRAMADO, MARKET_ABIERTO):
        mercado.status = MARKET_CERRADO
        db.commit()
    return _salida(mercado)


@router.post(
    "/{market_id}/resolve",
    response_model=MarketOut,
    responses={
        404: {"description": NO_EXISTE},
        409: {"description": "El mercado no esta cerrado o ya tiene otra opcion ganadora"},
        422: {"description": "La opcion ganadora no es una opcion del mercado"},
    },
)
def resolver(market_id: uuid.UUID, datos: MarketResolve, db: DbSession, response: Response) -> MarketOut:
    """Guarda la opcion ganadora de un mercado cerrado. No cambia el status: paga y marca HU2-10."""
    mercado = _bloquear(db, market_id)
    response.headers["Cache-Control"] = "no-store"
    if datos.winning_option not in {o["opcion"] for o in mercado.options}:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="La opción no pertenece al mercado")
    if mercado.winning_option is not None:
        if mercado.winning_option != datos.winning_option:
            raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="El mercado ya tiene otra opción ganadora")
        return _salida(mercado)
    if mercado.status != MARKET_CERRADO:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="El mercado no está cerrado")
    mercado.winning_option = datos.winning_option
    db.commit()
    return _salida(mercado)
