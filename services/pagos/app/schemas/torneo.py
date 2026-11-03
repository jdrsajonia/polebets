"""Contratos REST internos de apuestas de torneo (Motor → Pagos, HU2-09 y HU2-10)."""
import uuid
from decimal import Decimal
from typing import Literal

from pydantic import BaseModel, Field

from app.schemas.internal import MAX_BET_AMOUNT, InternalRequest


class BetTorneoIn(InternalRequest):
    bet_id: uuid.UUID  # clave de idempotencia (el Motor la deriva de usuario y mercado)
    user_id: uuid.UUID
    market_id: uuid.UUID
    option: str = Field(min_length=1, max_length=100)
    amount: int = Field(gt=0, le=MAX_BET_AMOUNT)  # fichas
    multiplier: Decimal = Field(gt=0, max_digits=6, decimal_places=2)
    lap: int | None = Field(default=None, ge=0)  # nulo si se apuesta antes de la largada


class BetTorneoSettlementIn(InternalRequest):
    result: Literal["won", "lost"]


class BetTorneoOut(BaseModel):
    bet_id: uuid.UUID
    event_id: uuid.UUID
    status: Literal["pending", "won", "lost"]
    payout: int | None  # fichas: monto × multiplicador si acertó, 0 si falló, nulo si pendiente
    nueva: bool
