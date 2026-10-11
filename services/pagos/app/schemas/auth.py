"""Schemas Pydantic para autenticacion (HU-01 registro, HU-02 login)."""
from uuid import UUID

from pydantic import BaseModel, EmailStr, Field


class RegisterRequest(BaseModel):
    """Datos de entrada para POST /auth/register (HU-01)."""

    name: str = Field(..., min_length=1, max_length=100)
    email: EmailStr
    password: str = Field(..., min_length=8, max_length=128)


class RegisterResponse(BaseModel):
    """Respuesta de POST /auth/register. Nunca expone password_hash."""

    id: UUID
    name: str
    email: EmailStr
    role: str
    balance: int

    model_config = {"from_attributes": True}