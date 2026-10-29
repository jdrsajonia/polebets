"""Modelos ORM de Pagos y usuarios. Ver documentation/modelo-datos-pagos.pdf."""
import uuid
from datetime import datetime
from decimal import Decimal

from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    DateTime,
    ForeignKey,
    Integer,
    Numeric,
    String,
    Uuid,
    func,
)
from sqlalchemy.orm import Mapped, mapped_column

from app.db import Base

ROLE_PLAYER = "player"
ROLE_ADMIN = "admin"

BET_PENDING = "pending"
BET_WON = "won"
BET_LOST = "lost"

LEDGER_PURCHASE = "purchase"
LEDGER_BET_DEBIT = "bet_debit"
LEDGER_BET_SETTLEMENT = "bet_settlement"
LEDGER_BUY_IN = "buy_in"  # HU2-06: pago de la inscripcion al torneo
LEDGER_REFUND = "refund"  # HU2-06: devolucion si el torneo se cancela
LEDGER_COMPLETED = "completed"


class User(Base):
    __tablename__ = "users"
    __table_args__ = (
        CheckConstraint(f"role IN ('{ROLE_PLAYER}', '{ROLE_ADMIN}')", name="ck_users_role"),
        CheckConstraint("balance >= 0", name="ck_users_balance_non_negative"),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True, default=uuid.uuid4, server_default=func.gen_random_uuid())
    email: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    password_hash: Mapped[str] = mapped_column(String(255), nullable=False)
    role: Mapped[str] = mapped_column(String(20), nullable=False, default=ROLE_PLAYER, server_default=ROLE_PLAYER)
    balance: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    points: Mapped[Decimal] = mapped_column(Numeric(12, 1), nullable=False, default=Decimal("0"), server_default="0")
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())


class Bet(Base):
    __tablename__ = "bets"
    __table_args__ = (
        CheckConstraint("amount > 0", name="ck_bets_amount_positive"),
        CheckConstraint(f"status IN ('{BET_PENDING}', '{BET_WON}', '{BET_LOST}')", name="ck_bets_status"),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True)  # lo genera el motor
    user_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("users.id"), nullable=False, index=True)
    market_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)  # sin FK: vive en MongoDB
    option: Mapped[str] = mapped_column(String(100), nullable=False)
    amount: Mapped[int] = mapped_column(BigInteger, nullable=False)
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=BET_PENDING, server_default=BET_PENDING)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())


class LedgerEntry(Base):
    __tablename__ = "ledger_entries"
    __table_args__ = (
        CheckConstraint(
            f"type IN ('{LEDGER_PURCHASE}', '{LEDGER_BET_DEBIT}', '{LEDGER_BET_SETTLEMENT}', "
            f"'{LEDGER_BUY_IN}', '{LEDGER_REFUND}')",
            name="ck_ledger_entries_type",
        ),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    user_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("users.id"), nullable=False, index=True)
    bet_id: Mapped[uuid.UUID | None] = mapped_column(ForeignKey("bets.id"), index=True)
    event_id: Mapped[str | None] = mapped_column(ForeignKey("events.id"), index=True)  # HU2-06
    type: Mapped[str] = mapped_column(String(20), nullable=False)
    tokens_delta: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    points_delta: Mapped[Decimal] = mapped_column(Numeric(12, 1), nullable=False, default=Decimal("0"), server_default="0")
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=LEDGER_COMPLETED, server_default=LEDGER_COMPLETED)
    idempotency_key: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())


# ---------------------------------------------------------------------------
# HU2-06: eventos y torneo multijugador (nombres del modelo de datos del P2)
#
# Un evento es una carrera simulada. Tiene dos estados:
#   status            la CARRERA:  programado -> en_curso -> finalizado
#   tournament_status el TORNEO:   abierto -> activo | cancelado -> repartido
# Se separan porque la carrera sigue (y las estaticas se liquidan) aunque el
# torneo se cancele por falta de inscritos.
#
# Las FICHAS del torneo no viven aqui: las maneja el Motor en Redis. Las
# columnas *_final, chips_wagered, prize y eligible las llena HU2-12 al
# repartir el botin. min_players y min_wager_pct no son columnas: se leen de
# la configuracion (EVENT_MIN_PLAYERS, EVENT_MIN_WAGER_PCT).
#
# T2-03: users pasa a otra base, por eso event_entries.user_id no es llave
# foranea.
# ---------------------------------------------------------------------------
EVENT_PROGRAMADO = "programado"
EVENT_EN_CURSO = "en_curso"
EVENT_FINALIZADO = "finalizado"

TOURNAMENT_ABIERTO = "abierto"
TOURNAMENT_ACTIVO = "activo"
TOURNAMENT_CANCELADO = "cancelado"
TOURNAMENT_REPARTIDO = "repartido"


class Event(Base):
    __tablename__ = "events"
    __table_args__ = (
        CheckConstraint(
            f"status IN ('{EVENT_PROGRAMADO}', '{EVENT_EN_CURSO}', '{EVENT_FINALIZADO}')",
            name="ck_events_status",
        ),
        CheckConstraint(
            f"tournament_status IN ('{TOURNAMENT_ABIERTO}', '{TOURNAMENT_ACTIVO}', "
            f"'{TOURNAMENT_CANCELADO}', '{TOURNAMENT_REPARTIDO}')",
            name="ck_events_tournament_status",
        ),
        CheckConstraint("buy_in > 0", name="ck_events_buy_in_positive"),
        CheckConstraint("initial_chips > 0", name="ck_events_initial_chips_positive"),
        CheckConstraint("commission_pct BETWEEN 0 AND 100", name="ck_events_commission_pct"),
        CheckConstraint("pot >= 0", name="ck_events_pot_non_negative"),
    )

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    race_key: Mapped[str] = mapped_column(String(64), nullable=False)  # carrera descargada (ej. 2024_monza)
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=EVENT_PROGRAMADO, server_default=EVENT_PROGRAMADO)
    tournament_status: Mapped[str] = mapped_column(
        String(20), nullable=False, default=TOURNAMENT_ABIERTO, server_default=TOURNAMENT_ABIERTO
    )
    buy_in: Mapped[int] = mapped_column(BigInteger, nullable=False)
    initial_chips: Mapped[int] = mapped_column(Integer, nullable=False)
    commission_pct: Mapped[int] = mapped_column(Integer, nullable=False)
    pot: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
    starts_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))  # largada
    ended_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))


class EventEntry(Base):
    __tablename__ = "event_entries"

    event_id: Mapped[str] = mapped_column(ForeignKey("events.id"), primary_key=True)
    user_id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True)  # sin FK: users vive en otra base (P2)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
    # Las llena HU2-12 al repartir el botin.
    chips_final: Mapped[int | None] = mapped_column(BigInteger)
    chips_wagered: Mapped[int | None] = mapped_column(BigInteger)
    position_final: Mapped[int | None] = mapped_column(Integer)
    prize: Mapped[int | None] = mapped_column(BigInteger)
    eligible: Mapped[bool | None] = mapped_column(Boolean)
