"""Contrato REST interno de mercados para el Motor (HU2-09 apostar, HU2-10 liquidar)."""
import uuid
from datetime import datetime
from typing import Literal

from pydantic import AwareDatetime, BaseModel, Field, model_validator


class OpcionOut(BaseModel):
    opcion: str
    multiplicador: float


class OpcionIn(BaseModel):
    opcion: str = Field(min_length=1, max_length=100)
    multiplicador: float = Field(gt=0)


class MarketCreate(BaseModel):
    """Alta de un mercado (HU2-07 estaticas, HU2-08 dinamicas). market_id lo genera quien llama."""

    market_id: uuid.UUID  # clave de idempotencia de la creacion
    event_id: uuid.UUID
    kind: Literal["estatica", "dinamica"]
    template: str = Field(min_length=1, max_length=100)
    question: str = Field(min_length=1, max_length=255)
    options: list[OpcionIn] = Field(min_length=2)
    status: Literal["programado", "abierto"] = "programado"
    opens_at: AwareDatetime | None = None
    closes_at: AwareDatetime | None = None
    race_close_s: int | None = Field(default=None, ge=0)

    @model_validator(mode="after")
    def _opciones_sin_repetir(self) -> "MarketCreate":
        nombres = [o.opcion for o in self.options]
        if len(set(nombres)) != len(nombres):
            raise ValueError("Los nombres de las opciones no se pueden repetir")
        return self


class MarketResolve(BaseModel):
    winning_option: str = Field(min_length=1, max_length=100)


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