"""Modelos ORM de PostgreSQL Usuarios (prototipo 2). Ver documentation/modelo-datos-p2.pdf.

users es la del prototipo 1 sin balance ni points: el saldo vive en
wallets, en PostgreSQL Pagos (T2-03).
"""
import uuid
from datetime import datetime

from sqlalchemy import CheckConstraint, DateTime, String, Uuid, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db import Base

ROLE_PLAYER = "player"
ROLE_ADMIN = "admin"


class User(Base):
    __tablename__ = "users"
    __table_args__ = (CheckConstraint(f"role IN ('{ROLE_PLAYER}', '{ROLE_ADMIN}')", name="ck_users_role"),)

    # Sin default en Python: en el registro el id lo genera el Orquestador (HU2-01).
    id: Mapped[uuid.UUID] = mapped_column(Uuid, primary_key=True, server_default=func.gen_random_uuid())
    email: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    password_hash: Mapped[str] = mapped_column(String(255), nullable=False)
    role: Mapped[str] = mapped_column(String(20), nullable=False, default=ROLE_PLAYER, server_default=ROLE_PLAYER)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
