"""Apuestas de torneo (Motor → Pagos, HU2-09 y HU2-10).

  POST /internal/bets/torneo                     registrar la apuesta (sin tokens ni ledger)
  POST /internal/bets/torneo/{bet_id}/settlement marcar won/lost y el pago en fichas

Ruta aparte de /internal/debit y /internal/settlements: esas mueven tokens y su
idempotencia es la clave del ledger (bet-debit:, bet-settlement:). Las fichas
viven en Redis (HU2-11), asi que aqui no hay saldo ni ledger: la clave es el
bet_id y el estado de la propia apuesta.

Errores 409 con {"codigo", "mensaje"}: mercado_cerrado, ya_apostaste,
datos_distintos.
"""
import uuid

from fastapi import APIRouter, HTTPException, Response, status

from app.deps import DbSession, ServicioInterno
from app.schemas.torneo import BetTorneoIn, BetTorneoOut, BetTorneoSettlementIn
from app.services.liquidacion import (
    ApuestaDuplicada,
    ApuestaNoExiste,
    BetDebitData,
    ClaveReutilizada,
    MercadoCerrado,
    MercadoNoExiste,
    ModoNoSoportado,
    ResultadoTorneo,
    liquidar_apuesta_torneo,
    registrar_apuesta_torneo,
)

router = APIRouter(prefix="/internal/bets/torneo", tags=["torneo"], dependencies=[ServicioInterno])

MERCADO_CERRADO = {"codigo": "mercado_cerrado", "mensaje": "El mercado no está abierto"}


def _conflicto(codigo: str, mensaje: str) -> HTTPException:
    return HTTPException(status_code=status.HTTP_409_CONFLICT, detail={"codigo": codigo, "mensaje": mensaje})


def _salida(resultado: ResultadoTorneo, response: Response) -> BetTorneoOut:
    if not resultado.nueva:
        response.status_code = status.HTTP_200_OK
    a = resultado.apuesta
    return BetTorneoOut(bet_id=a.id, event_id=a.event_id, status=a.status, payout=a.payout, nueva=resultado.nueva)


@router.post(
    "",
    response_model=BetTorneoOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Reenvío con el mismo bet_id y los mismos datos"},
        401: {"description": "Token de servicio ausente o inválido"},
        404: {"description": "Mercado inexistente"},
        409: {"description": "mercado_cerrado, ya_apostaste o datos_distintos"},
    },
)
def registrar(datos: BetTorneoIn, db: DbSession, response: Response) -> BetTorneoOut:
    try:
        resultado = registrar_apuesta_torneo(
            db,
            BetDebitData(
                bet_id=datos.bet_id,
                user_id=datos.user_id,
                market_id=datos.market_id,
                option=datos.option,
                amount=datos.amount,
                multiplier=datos.multiplier,
                lap=datos.lap,
                idempotency_key=str(datos.bet_id),
            ),
        )
    except MercadoNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Mercado inexistente") from None
    except MercadoCerrado:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=MERCADO_CERRADO) from None
    except ApuestaDuplicada:
        raise _conflicto("ya_apostaste", "Ya apostó en este mercado") from None
    except ClaveReutilizada:
        raise _conflicto("datos_distintos", "La apuesta ya existe con otros datos") from None
    return _salida(resultado, response)


@router.post(
    "/{bet_id}/settlement",
    response_model=BetTorneoOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Ya estaba liquidada con el mismo resultado"},
        401: {"description": "Token de servicio ausente o inválido"},
        404: {"description": "Apuesta inexistente"},
        409: {"description": "datos_distintos: ya liquidada con otro resultado"},
        422: {"description": "La apuesta es individual (va por /internal/settlements)"},
    },
)
def liquidar(bet_id: uuid.UUID, datos: BetTorneoSettlementIn, db: DbSession, response: Response) -> BetTorneoOut:
    try:
        resultado = liquidar_apuesta_torneo(db, bet_id, datos.result)
    except ApuestaNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Apuesta inexistente") from None
    except ModoNoSoportado:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Las apuestas individuales se liquidan en /internal/settlements"
        ) from None
    except ClaveReutilizada:
        raise _conflicto("datos_distintos", "La apuesta ya se liquidó con otro resultado") from None
    return _salida(resultado, response)
