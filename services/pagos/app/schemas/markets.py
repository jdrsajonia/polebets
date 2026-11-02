"""Contrato REST interno para leer un mercado (HU2-09: lo consulta el Motor al apostar)."""
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
