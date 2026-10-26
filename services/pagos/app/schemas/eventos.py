"""Contratos REST internos de HU2-06 (Motor y Orquestador -> Pagos)."""
import uuid
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

EstadoEvento = Literal["registration", "multiplayer", "individual_only", "finished"]


class _Entrada(BaseModel):
    model_config = ConfigDict(extra="forbid")


class EventCreateIn(_Entrada):
    """Lo envia el Motor al arrancar la simulacion. Lo omitido usa los valores por defecto."""

    event_id: str = Field(min_length=1, max_length=64, pattern=r"^[A-Za-z0-9_.:-]+$")
    name: str = Field(min_length=1, max_length=150)
    race_ref: str = Field(min_length=1, max_length=64)
    entry_fee: int | None = Field(default=None, ge=100, le=1_000_000)
    initial_chips: int | None = Field(default=None, gt=0, le=1_000_000)
    commission_pct: int | None = Field(default=None, ge=0, le=100)
    min_wager_pct: int | None = Field(default=None, ge=0, le=100)
    min_players: int | None = Field(default=None, ge=2, le=1000)


class EventOut(BaseModel):
    event_id: str
    name: str
    race_ref: str
    status: EstadoEvento
    entry_fee: int
    initial_chips: int
    commission_pct: int
    min_wager_pct: int
    min_players: int
    pot: int
    registered: int
    created_at: datetime
    started_at: datetime | None


class RegistrationIn(_Entrada):
    user_id: uuid.UUID


class RegistrationOut(BaseModel):
    event_id: str
    user_id: uuid.UUID
    status: Literal["active", "refunded"]
    fee: int
    initial_chips: int
    pot: int
    saldo: int | None = None  # solo al inscribirse: saldo de PoleCoins que queda
    nueva: bool = False


class EventStartOut(BaseModel):
    event_id: str
    status: EstadoEvento
    pot: int
    initial_chips: int
    players: list[uuid.UUID]  # inscritos activos: el Motor les asigna las fichas en Redis
    refunded: int
    nueva: bool
