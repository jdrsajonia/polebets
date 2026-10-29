"""Modelos ORM de PostgreSQL Pagos (prototipo 2). Ver documentation/modelo-datos-p2.pdf.

users vive en PostgreSQL Usuarios: aqui user_id es un uuid sin llave foranea.
"""
import uuid
from datetime import datetime
from decimal import Decimal
from typing import Any

from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    DateTime,
    ForeignKey,
    Integer,
    Numeric,
    String,
    UniqueConstraint,
    Uuid,
    func,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Mapped, mapped_column

from app.db import Base


def _valores(*valores: str) -> str:
    return ", ".join(f"'{v}'" for v in valores)


EVENT_PROGRAMADO = "programado"
EVENT_EN_CURSO = "en_curso"
EVENT_FINALIZADO = "finalizado"

TORNEO_ABIERTO = "abierto"
TORNEO_ACTIVO = "activo"
TORNEO_CANCELADO = "cancelado"
TORNEO_REPARTIDO = "repartido"

MARKET_ESTATICA = "estatica"
MARKET_DINAMICA = "dinamica"

MARKET_PROGRAMADO = "programado"
MARKET_ABIERTO = "abierto"
MARKET_CERRADO = "cerrado"
MARKET_LIQUIDADO = "liquidado"

BET_INDIVIDUAL = "individual"
BET_TORNEO = "torneo"

BET_PENDING = "pending"
BET_WON = "won"
BET_LOST = "lost"

LEDGER_PURCHASE = "purchase"
LEDGER_DEBIT = "debit"
LEDGER_SETTLE = "settle"
LEDGER_BUY_IN = "buy_in"
LEDGER_REFUND = "refund"
LEDGER_PRIZE = "prize"
LEDGER_COMPLETED = "completed"


class Wallet(Base):
    __tablename__ = "wallets"
    __table_args__ = (CheckConstraint("balance >= 0", name="ck_wallets_balance_non_negative"),)

    user_id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True)
    balance: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now(), onupdate=func.now()
    )


class Event(Base):
    __tablename__ = "events"
    __table_args__ = (
        CheckConstraint(
            f"status IN ({_valores(EVENT_PROGRAMADO, EVENT_EN_CURSO, EVENT_FINALIZADO)})",
            name="ck_events_status",
        ),
        CheckConstraint(
            f"tournament_status IN ({_valores(TORNEO_ABIERTO, TORNEO_ACTIVO, TORNEO_CANCELADO, TORNEO_REPARTIDO)})",
            name="ck_events_tournament_status",
        ),
        CheckConstraint("buy_in > 0", name="ck_events_buy_in_positive"),
        CheckConstraint("initial_chips > 0", name="ck_events_initial_chips_positive"),
        CheckConstraint("commission_pct >= 0 AND commission_pct <= 100", name="ck_events_commission_pct_range"),
        CheckConstraint("pot >= 0", name="ck_events_pot_non_negative"),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True, default=uuid.uuid4, server_default=func.gen_random_uuid())
    race_key: Mapped[str] = mapped_column(String(100), nullable=False)  # carrera en MongoDB
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=EVENT_PROGRAMADO, server_default=EVENT_PROGRAMADO)
    starts_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    ended_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    buy_in: Mapped[int] = mapped_column(BigInteger, nullable=False)
    initial_chips: Mapped[int] = mapped_column(BigInteger, nullable=False)
    commission_pct: Mapped[Decimal] = mapped_column(Numeric(5, 2), nullable=False)  # 10 = 10 %
    pot: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    tournament_status: Mapped[str] = mapped_column(
        String(20), nullable=False, default=TORNEO_ABIERTO, server_default=TORNEO_ABIERTO
    )


class EventEntry(Base):
    """Un inscrito al torneo. Las columnas de resultado las llena el cierre del evento (RF-16)."""

    __tablename__ = "event_entries"

    event_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("events.id"), primary_key=True)
    user_id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True)
    chips_final: Mapped[int | None] = mapped_column(BigInteger)
    chips_wagered: Mapped[int | None] = mapped_column(BigInteger)
    position_final: Mapped[int | None] = mapped_column(Integer)
    prize: Mapped[int | None] = mapped_column(BigInteger)
    eligible: Mapped[bool | None] = mapped_column(Boolean)


class Market(Base):
    __tablename__ = "markets"
    __table_args__ = (
        CheckConstraint(f"kind IN ({_valores(MARKET_ESTATICA, MARKET_DINAMICA)})", name="ck_markets_kind"),
        CheckConstraint(
            f"status IN ({_valores(MARKET_PROGRAMADO, MARKET_ABIERTO, MARKET_CERRADO, MARKET_LIQUIDADO)})",
            name="ck_markets_status",
        ),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True, default=uuid.uuid4, server_default=func.gen_random_uuid())
    event_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("events.id"), nullable=False, index=True)
    kind: Mapped[str] = mapped_column(String(20), nullable=False)
    template: Mapped[str] = mapped_column(String(100), nullable=False)
    question: Mapped[str] = mapped_column(String(255), nullable=False)
    # Lista de opciones, cada una con su multiplicador: [{"opcion": "VER", "multiplicador": 2.5}, ...]
    options: Mapped[list[dict[str, Any]]] = mapped_column(JSONB, nullable=False)
    opens_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    closes_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    race_close_s: Mapped[int | None] = mapped_column(Integer)  # segundo de carrera del cierre
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=MARKET_PROGRAMADO, server_default=MARKET_PROGRAMADO)
    winning_option: Mapped[str | None] = mapped_column(String(100))


class Bet(Base):
    __tablename__ = "bets"
    __table_args__ = (
        UniqueConstraint("user_id", "market_id", name="uq_bets_user_market"),
        CheckConstraint(f"mode IN ({_valores(BET_INDIVIDUAL, BET_TORNEO)})", name="ck_bets_mode"),
        CheckConstraint("amount > 0", name="ck_bets_amount_positive"),
        CheckConstraint("multiplier > 0", name="ck_bets_multiplier_positive"),
        CheckConstraint("payout >= 0", name="ck_bets_payout_non_negative"),
        CheckConstraint(f"status IN ({_valores(BET_PENDING, BET_WON, BET_LOST)})", name="ck_bets_status"),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True)  # lo genera el motor
    user_id: Mapped[uuid.UUID] = mapped_column(Uuid, nullable=False)  # lo indexa uq_bets_user_market
    market_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("markets.id"), nullable=False, index=True)
    event_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("events.id"), nullable=False, index=True)
    mode: Mapped[str] = mapped_column(String(20), nullable=False)
    option: Mapped[str] = mapped_column(String(100), nullable=False)
    amount: Mapped[int] = mapped_column(BigInteger, nullable=False)  # tokens (individual) o fichas (torneo)
    multiplier: Mapped[Decimal] = mapped_column(Numeric(6, 2), nullable=False)
    lap: Mapped[int | None] = mapped_column(Integer)  # nulo si se apuesta antes de la largada
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=BET_PENDING, server_default=BET_PENDING)
    payout: Mapped[int | None] = mapped_column(BigInteger)  # nulo mientras esta pendiente
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())


class LedgerEntry(Base):
    """Movimientos de tokens. Las fichas no son tokens y no pasan por aqui."""

    __tablename__ = "ledger_entries"
    __table_args__ = (
        CheckConstraint(
            f"type IN ({_valores(LEDGER_PURCHASE, LEDGER_DEBIT, LEDGER_SETTLE, LEDGER_BUY_IN, LEDGER_REFUND, LEDGER_PRIZE)})",
            name="ck_ledger_entries_type",
        ),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    user_id: Mapped[uuid.UUID] = mapped_column(Uuid, nullable=False, index=True)
    bet_id: Mapped[uuid.UUID | None] = mapped_column(ForeignKey("bets.id"), index=True)
    event_id: Mapped[uuid.UUID | None] = mapped_column(ForeignKey("events.id"), index=True)
    type: Mapped[str] = mapped_column(String(20), nullable=False)
    tokens_delta: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=LEDGER_COMPLETED, server_default=LEDGER_COMPLETED)
    idempotency_key: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
