"""Endpoints internos de la billetera (T2-03). Los llama el Orquestador.

    POST /internal/wallets            crea la billetera con saldo 0 (registro, HU2-01)
    GET  /internal/wallets/{user_id}  saldo y tasa (pagina SSR y billetera)
"""
import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Response, status

from app.deps import DbSession, ServicioInterno
from app.schemas.wallets import WalletIn, WalletOut
from app.services.billetera import crear_billetera, saldo_de, tasa

router = APIRouter(prefix="/internal/wallets", tags=["wallets"], dependencies=[ServicioInterno])


def _sin_cache(response: Response) -> None:
    # El saldo cambia a cada rato: que ningun proxy lo guarde.
    response.headers["Cache-Control"] = "no-store"


@router.post(
    "",
    response_model=WalletOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "La billetera ya existía; no se modifica"},
        400: {"description": "Idempotency-Key distinta de wallet:{user_id}"},
        401: {"description": "Token de servicio ausente o inválido"},
    },
)
def crear(
    datos: WalletIn,
    db: DbSession,
    response: Response,
    idempotency_key: Annotated[str | None, Header(alias="Idempotency-Key")] = None,
) -> WalletOut:
    """Crea la billetera con saldo 0. Repetirla con la misma clave responde 200 sin cambiar nada."""
    if idempotency_key != f"wallet:{datos.user_id}":
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="Idempotency-Key inválida")
    billetera, nueva = crear_billetera(db, datos.user_id)
    if not nueva:
        response.status_code = status.HTTP_200_OK
    return WalletOut(user_id=billetera.user_id, balance=billetera.balance, cop_por_token=tasa())


@router.get(
    "/{user_id}",
    response_model=WalletOut,
    responses={
        401: {"description": "Token de servicio ausente o inválido"},
        404: {"description": "El usuario no tiene billetera"},
    },
)
def ver(user_id: uuid.UUID, db: DbSession, response: Response) -> WalletOut:
    saldo = saldo_de(db, user_id)
    if saldo is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Billetera inexistente")
    _sin_cache(response)
    return WalletOut(user_id=user_id, balance=saldo, cop_por_token=tasa())
