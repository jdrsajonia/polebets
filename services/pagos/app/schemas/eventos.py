"""Contratos REST internos de HU2-06 (Motor y Orquestador -> Pagos).

Nombres del modelo de datos del P2 (ver documentation/orquestador-api.md).
"""
import uuid
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

EstadoCarrera = Literal["programado", "en_curso", "finalizado"]
EstadoTorneo = Literal["abierto", "activo", "cancelado", "repartido"]


class _Entrada(BaseModel):
    model_config = ConfigDict(extra="forbid")


class EventCreateIn(_Entrada):
    """Crear evento al arrancar la simulacion. Lo omitido usa la configuracion."""

    event_id: str = Field(min_length=1, max_length=64, pattern=r"^[A-Za-z0-9_.:-]+$")
    race_key: str = Field(min_length=1, max_length=64)
    buy_in: int | None = Field(default=None, gt=0, le=1_000_000)
    initial_chips: int | None = Field(default=None, gt=0, le=1_000_000)
    commission_pct: int | None = Field(default=None, ge=0, le=100)


class EventOut(BaseModel):
    event_id: str
    race_key: str
    status: EstadoCarrera
    tournament_status: EstadoTorneo
    buy_in: int
    initial_chips: int
    commission_pct: int
    pot: int
    registered: int  # filas en event_entries (incluye las de un torneo cancelado)
    created_at: datetime
    starts_at: datetime | None
    ended_at: datetime | None


class RegistrationIn(_Entrada):
    user_id: uuid.UUID


class RegistrationOut(BaseModel):
    event_id: str
    user_id: uuid.UUID
    tournament_status: EstadoTorneo
    refunded: bool  # inscrito y torneo cancelado: se le devolvio el buy_in
    buy_in: int
    initial_chips: int
    pot: int
    saldo: int | None = None  # solo al inscribirse: PoleCoins que le quedan
    nueva: bool = False


class EventStartOut(BaseModel):
    event_id: str
    status: EstadoCarrera
    tournament_status: EstadoTorneo
    pot: int
    initial_chips: int
    players: list[uuid.UUID]  # inscritos del torneo activo: el Motor les asigna las fichas en Redis
    refunded: int
    nueva: bool
