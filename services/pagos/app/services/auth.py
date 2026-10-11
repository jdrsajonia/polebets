"""Logica de negocio de autenticacion (HU-01: registro)."""
from fastapi import HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.core.security import hash_password
from app.models import ROLE_PLAYER, User
from app.schemas.auth import RegisterRequest


def registrar_usuario(db: Session, data: RegisterRequest) -> User:
    """Crea un usuario nuevo con rol player y saldo 0.

    Cumple HU-01:
      - Correo unico: si ya existe, 409 con mensaje claro.
      - Contrasena hasheada con bcrypt (RNF-06), nunca en texto plano.
      - Rol por defecto "player".
      - Saldo inicial 0.
    """
    # 1. Verificar correo unico. El mensaje es exacto segun el criterio
    #    de aceptacion de HU-01: "Este correo ya está registrado".
    existente = db.scalar(select(User).where(User.email == data.email))
    if existente is not None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Este correo ya está registrado",
        )

    # 2. Hash de la contrasena (RNF-06).
    password_hash = hash_password(data.password)

    # 3. Crear el usuario con rol player y saldo 0.
    user = User(
        email=data.email,
        name=data.name,
        password_hash=password_hash,
        role=ROLE_PLAYER,
        balance=0,
    )
    db.add(user)
    db.commit()
    db.refresh(user)
    return user