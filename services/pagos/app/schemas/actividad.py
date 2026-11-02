"""Esquemas de la actividad de un jugador para la pagina SSR (HU2-03).

Los nombres son los de la base (en ingles), como en el resto de /internal/*;
el Orquestador los traduce a la API publica (documentation/orquestador-api.md, §3.2).
"""
import uuid
from datetime import datetime

from pydantic import BaseModel


class EventEntryOut(BaseModel):
    """Un evento en el que el jugador esta inscrito, con su resultado si ya se reparte."""

    event_id: uuid.UUID
    race_key: str
    status: str  # programado | en_curso | finalizado
    tournament_status: str  # abierto | activo | cancelado | repartido
    starts_at: datetime
    chips_final: int | None
    position_final: int | None
    prize: int | None


class BetOut(BaseModel):
    bet_id: uuid.UUID
    market_id: uuid.UUID
    event_id: uuid.UUID
    option: str
    amount: int
    mode: str  # individual | torneo
    multiplier: float
    status: str
    created_at: datetime


class LedgerOut(BaseModel):
    type: str  # purchase | debit | settle | buy_in | refund | prize
    tokens_delta: int
    event_id: uuid.UUID | None
    created_at: datetime
