"""Esquemas de Usuarios (HU2-01, HU2-02).

Los nombres publicos van en español (`nombre`, `rol`, `contrasena`) porque asi
los usan el front y el contrato del Orquestador (documentation/orquestador-api.md).
"""
import uuid
from typing import Literal

from pydantic import BaseModel, ConfigDict, EmailStr, Field, field_validator

from app.models import User

LARGO_MINIMO_CONTRASENA = 8


class UsuarioOut(BaseModel):
    """El objeto `usuario` del contrato: {id, nombre, email, rol}. Nunca la contraseña."""

    id: uuid.UUID
    nombre: str
    email: str
    rol: Literal["player", "admin"]

    @classmethod
    def desde_modelo(cls, user: User) -> "UsuarioOut":
        return cls(id=user.id, nombre=user.name, email=user.email, rol=user.role)


class LoginIn(BaseModel):
    """Cuerpo de POST /auth/login.

    El formato del correo NO se valida aqui a proposito: un correo mal escrito
    debe recibir la misma respuesta que cualquier credencial invalida
    («Correo o contraseña incorrectos»). Validar el formato es del registro.
    """

    email: str = Field(min_length=3, max_length=255)
    password: str = Field(min_length=1, max_length=128)


class CrearUsuarioIn(BaseModel):
    """Cuerpo de POST /internal/users (paso 2 del registro orquestado, HU2-01).

    El `id` lo genera el Orquestador: la billetera en Pagos ya se creo con ese
    mismo id en el paso 1, asi que Usuarios lo guarda tal cual.
    """

    model_config = ConfigDict(extra="forbid")

    id: uuid.UUID
    nombre: str = Field(min_length=1, max_length=100)
    email: EmailStr
    contrasena: str = Field(min_length=LARGO_MINIMO_CONTRASENA, max_length=128)

    @field_validator("nombre")
    @classmethod
    def _nombre_sin_espacios(cls, valor: str) -> str:
        valor = valor.strip()
        if not valor:
            raise ValueError("El nombre no puede estar vacío")
        return valor
