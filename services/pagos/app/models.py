"""Modelos ORM de Pagos y usuarios. Ver documentation/modelo-datos-pagos.pdf."""
import uuid
from datetime import datetime
from decimal import Decimal

from sqlalchemy import (
    BigInteger,
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
LEDGER_EVENT_ENTRY = "event_entry"  # HU2-06: pago de la inscripcion al multijugador
LEDGER_EVENT_REFUND = "event_refund"  # HU2-06: devolucion si hay menos del minimo de inscritos
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
            f"'{LEDGER_EVENT_ENTRY}', '{LEDGER_EVENT_REFUND}')",
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
# HU2-06: eventos multijugador e inscripciones
#
# Un evento es una carrera simulada. Lo crea el Motor cuando el admin arranca
# la simulacion. Los jugadores pagan la inscripcion en PoleCoins (va al botin)
# hasta la largada. Al largar, con menos de min_players inscritos se devuelven
# las inscripciones y el evento queda solo en modo individual.
#
# Las FICHAS del torneo (initial_chips por jugador) NO viven aqui: son fichas de
# juego, no PoleCoins, y las maneja el Motor en Redis. Aqui solo se guarda
# cuantas recibe cada inscrito para que todos usen el mismo valor.
#
# T2-03: cuando Usuarios y Pagos se separen, user_id deja de ser llave foranea.
# ---------------------------------------------------------------------------
EVENT_REGISTRATION = "registration"  # inscripciones abiertas (antes de la largada)
EVENT_MULTIPLAYER = "multiplayer"  # largo con suficientes inscritos
EVENT_INDIVIDUAL_ONLY = "individual_only"  # largo con pocos inscritos: se devolvio todo
EVENT_FINISHED = "finished"  # botin repartido (HU2-12)

REGISTRATION_ACTIVE = "active"
REGISTRATION_REFUNDED = "refunded"


class Event(Base):
    __tablename__ = "events"
    __table_args__ = (
        CheckConstraint(
            f"status IN ('{EVENT_REGISTRATION}', '{EVENT_MULTIPLAYER}', "
            f"'{EVENT_INDIVIDUAL_ONLY}', '{EVENT_FINISHED}')",
            name="ck_events_status",
        ),
        CheckConstraint("entry_fee >= 100", name="ck_events_entry_fee_min"),
        CheckConstraint("initial_chips > 0", name="ck_events_initial_chips_positive"),
        CheckConstraint("commission_pct BETWEEN 0 AND 100", name="ck_events_commission_pct"),
        CheckConstraint("min_wager_pct BETWEEN 0 AND 100", name="ck_events_min_wager_pct"),
        CheckConstraint("min_players >= 2", name="ck_events_min_players"),
        CheckConstraint("pot >= 0", name="ck_events_pot_non_negative"),
    )

    id: Mapped[str] = mapped_column(String(64), primary_key=True)  # lo genera el Motor
    name: Mapped[str] = mapped_column(String(150), nullable=False)
    race_ref: Mapped[str] = mapped_column(String(64), nullable=False)  # carrera de OpenF1
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=EVENT_REGISTRATION, server_default=EVENT_REGISTRATION)
    entry_fee: Mapped[int] = mapped_column(BigInteger, nullable=False)
    initial_chips: Mapped[int] = mapped_column(Integer, nullable=False)
    commission_pct: Mapped[int] = mapped_column(Integer, nullable=False)
    min_wager_pct: Mapped[int] = mapped_column(Integer, nullable=False)
    min_players: Mapped[int] = mapped_column(Integer, nullable=False)
    pot: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0, server_default="0")
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))


class EventRegistration(Base):
    __tablename__ = "event_registrations"
    __table_args__ = (
        UniqueConstraint("event_id", "user_id", name="uq_event_registrations_event_user"),
        CheckConstraint("fee > 0", name="ck_event_registrations_fee_positive"),
        CheckConstraint(
            f"status IN ('{REGISTRATION_ACTIVE}', '{REGISTRATION_REFUNDED}')",
            name="ck_event_registrations_status",
        ),
    )

    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True, default=uuid.uuid4)
    event_id: Mapped[str] = mapped_column(ForeignKey("events.id"), nullable=False, index=True)
    user_id: Mapped[uuid.UUID] = mapped_column(ForeignKey("users.id"), nullable=False, index=True)
    fee: Mapped[int] = mapped_column(BigInteger, nullable=False)
    status: Mapped[str] = mapped_column(String(20), nullable=False, default=REGISTRATION_ACTIVE, server_default=REGISTRATION_ACTIVE)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
