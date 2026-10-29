"""Contratos REST internos de apuestas individuales (Motor → Pagos)."""
import uuid
from decimal import Decimal
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

MAX_BET_AMOUNT = 2**31 - 1


class InternalRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")


class BetDebitIn(InternalRequest):
    bet_id: uuid.UUID
    user_id: uuid.UUID
    market_id: uuid.UUID
    option: str = Field(min_length=1, max_length=100)
    amount: int = Field(gt=0, le=MAX_BET_AMOUNT)
    multiplier: Decimal = Field(gt=0, max_digits=6, decimal_places=2)
    lap: int | None = Field(default=None, ge=0)  # nulo si se apuesta antes de la largada
    idempotency_key: str = Field(min_length=1, max_length=255)


class BetDebitOut(BaseModel):
    bet_id: uuid.UUID
    event_id: uuid.UUID
    status: Literal["pending", "won", "lost"]
    saldo: int
    nueva: bool


class BetSettlementIn(InternalRequest):
    bet_id: uuid.UUID
    result: Literal["won", "lost"]
    idempotency_key: str = Field(min_length=1, max_length=255)


class BetSettlementOut(BaseModel):
    bet_id: uuid.UUID
    status: Literal["won", "lost"]
    payout: int  # tokens recibidos: monto × multiplicador si acertó, 0 si falló
    saldo: int
    nueva: bool
