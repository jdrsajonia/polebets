"""Cuentas de usuario para el Orquestador (HU2-01 registro, HU2-03 pagina SSR).

Regla del registro orquestado (documentation/orquestador-api.md, §3.1 y §5):
el Orquestador genera el id, crea primero la billetera en Pagos y despues
llama aqui. Si un intento se corta a la mitad, el Orquestador lo repite con el
MISMO id, asi que crear debe ser idempotente:

  - id nuevo y correo libre       -> se crea (201)
  - mismo id con el mismo correo  -> ya existia, no se toca nada (200)
  - correo usado por otra cuenta  -> CorreoRegistrado (409)
"""
import uuid

from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.core.security import hash_password
from app.models import ROLE_PLAYER, User
from app.schemas.usuarios import CrearUsuarioIn
from app.services.auth import buscar_por_email, normalizar_email

MENSAJE_CORREO_REGISTRADO = "Este correo ya está registrado"


class CorreoRegistrado(Exception):
    """El correo ya pertenece a otra cuenta (o el id ya se uso con otro correo)."""


def _repeticion_de(existente: User | None, email: str) -> bool:
    return existente is not None and existente.email == email


def crear_usuario(db: Session, datos: CrearUsuarioIn) -> tuple[User, bool]:
    """Crea un jugador con el id que manda el Orquestador. Devuelve (usuario, nuevo)."""
    email = normalizar_email(str(datos.email))

    # 1. Reintento del Orquestador con el mismo id: responder lo que ya existe.
    por_id = db.get(User, datos.id)
    if por_id is not None:
        if _repeticion_de(por_id, email):
            return por_id, False
        raise CorreoRegistrado  # id ya usado con otro correo: no deberia pasar

    # 2. Chequeo rapido del correo (case-insensitive) para el caso normal.
    if buscar_por_email(db, email) is not None:
        raise CorreoRegistrado

    user = User(
        id=datos.id,
        email=email,
        name=datos.nombre,
        password_hash=hash_password(datos.contrasena),  # nunca en texto plano (RNF-06)
        role=ROLE_PLAYER,  # el registro publico siempre crea jugadores
    )
    db.add(user)
    try:
        db.commit()
    except IntegrityError:
        # Dos peticiones a la vez: el UNIQUE de Postgres decide quien gana.
        db.rollback()
        if _repeticion_de(db.get(User, datos.id), email):
            return db.get(User, datos.id), False
        raise CorreoRegistrado from None
    db.refresh(user)
    return user, True


def obtener_usuario(db: Session, user_id: uuid.UUID) -> User | None:
    return db.get(User, user_id)
