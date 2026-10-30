"""Endpoints REST internos de apuestas individuales (Motor → Pagos)."""
import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Response, status

from app.deps import DbSession, ServicioInterno
from app.schemas.internal import (
    BetDebitIn,
    BetDebitOut,
    BetSettlementIn,
    BetSettlementOut,
)
from app.services.liquidacion import (
    ApuestaDuplicada,
    ApuestaNoExiste,
    BetDebitData,
    BilleteraNoExiste,
    ClaveReutilizada,
    MercadoNoExiste,
    ModoNoSoportado,
    ResultadoInvalido,
    SaldoInsuficiente,
    debitar_apuesta,
    liquidar_apuesta,
)

router = APIRouter(prefix="/internal", tags=["internal"], dependencies=[ServicioInterno])


def _validar_clave(header: str | None, cuerpo: str, prefijo: str, bet_id: uuid.UUID) -> None:
    esperada = f"{prefijo}:{bet_id}"
    if header != cuerpo or cuerpo != esperada:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="Idempotency-Key inválida")


@router.post(
    "/debit",
    response_model=BetDebitOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Reenvío idempotente del débito"},
        400: {"description": "Clave de idempotencia inválida"},
        401: {"description": "Token de servicio ausente o inválido"},
        404: {"description": "Billetera o mercado inexistente"},
        409: {"description": "ID o clave reutilizados con otro contenido, o ya apostó en ese mercado"},
        422: {"description": "Saldo insuficiente o cuerpo inválido"},
    },
)
def debitar(
    datos: BetDebitIn,
    db: DbSession,
    response: Response,
    idempotency_key: Annotated[str | None, Header(alias="Idempotency-Key")] = None,
) -> BetDebitOut:
    _validar_clave(idempotency_key, datos.idempotency_key, "bet-debit", datos.bet_id)
    try:
        resultado = debitar_apuesta(
            db,
            BetDebitData(
                bet_id=datos.bet_id,
                user_id=datos.user_id,
                market_id=datos.market_id,
                option=datos.option,
                amount=datos.amount,
                multiplier=datos.multiplier,
                lap=datos.lap,
                idempotency_key=datos.idempotency_key,
            ),
        )
    except BilleteraNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billetera inexistente") from None
    except MercadoNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Mercado inexistente") from None
    except SaldoInsuficiente:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Saldo insuficiente") from None
    except ClaveReutilizada:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Clave reutilizada") from None
    except ApuestaDuplicada:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Ya apostó en este mercado") from None

    if not resultado.nueva:
        response.status_code = status.HTTP_200_OK
    return BetDebitOut(
        bet_id=resultado.apuesta.id,
        event_id=resultado.apuesta.event_id,
        status=resultado.apuesta.status,
        saldo=resultado.saldo,
        nueva=resultado.nueva,
    )


@router.post(
    "/settlements",
    response_model=BetSettlementOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Reenvío idempotente de la liquidación"},
        400: {"description": "Clave de idempotencia inválida"},
        401: {"description": "Token de servicio ausente o inválido"},
        404: {"description": "Apuesta o billetera inexistente"},
        409: {"description": "Apuesta o clave ya usadas con otro resultado"},
        422: {"description": "Resultado inválido o apuesta de torneo"},
    },
)
def liquidar(
    datos: BetSettlementIn,
    db: DbSession,
    response: Response,
    idempotency_key: Annotated[str | None, Header(alias="Idempotency-Key")] = None,
) -> BetSettlementOut:
    _validar_clave(idempotency_key, datos.idempotency_key, "bet-settlement", datos.bet_id)
    try:
        resultado = liquidar_apuesta(db, datos.bet_id, datos.result, datos.idempotency_key)
    except ApuestaNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Apuesta inexistente") from None
    except BilleteraNoExiste:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billetera inexistente") from None
    except ClaveReutilizada:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Liquidación ya procesada con otro contenido") from None
    except ModoNoSoportado:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Las apuestas de torneo no se liquidan en tokens") from None
    except ResultadoInvalido:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Resultado inválido") from None

    if not resultado.nueva:
        response.status_code = status.HTTP_200_OK
    return BetSettlementOut(
        bet_id=resultado.apuesta.id,
        status=resultado.apuesta.status,
        payout=resultado.apuesta.payout,
        saldo=resultado.saldo,
        nueva=resultado.nueva,
    )
