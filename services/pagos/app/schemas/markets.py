"""Contrato REST interno de mercados para el Motor (HU2-09 apostar, HU2-10 liquidar)."""
import uuid
from datetime import datetime
from typing import Literal

from pydantic import BaseModel


class OpcionOut(BaseModel):
    opcion: str
    multiplicador: float


class MarketOut(BaseModel):
    market_id: uuid.UUID
    event_id: uuid.UUID
    kind: Literal["estatica", "dinamica"]
    template: str
    question: str
    options: list[OpcionOut]
    status: Literal["programado", "abierto", "cerrado", "liquidado"]
    opens_at: datetime | None
    closes_at: datetime | None
    race_close_s: int | None  # segundo de carrera del cierre (dinamicas)
    winning_option: str | None


class MarketBetOut(BaseModel):
    """Una apuesta del mercado, lo que el Motor necesita para liquidarla (HU2-10)."""

    bet_id: uuid.UUID
    user_id: uuid.UUID
    mode: Literal["individual", "torneo"]
    option: str
    amount: int
    multiplier: float
    status: Literal["pending", "won", "lost"]
    payout: int | None


class MarketLiquidatedOut(BaseModel):
    market_id: uuid.UUID
    status: Literal["liquidado"]
    nueva: bool