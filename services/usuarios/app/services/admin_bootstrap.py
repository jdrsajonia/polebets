"""Administrador inicial (RF-21).

Con la separacion de bases (T2-03) la tabla users paso de Pagos a Usuarios, y
con ella la creacion del administrador del prototipo 1 (HU-12). Al arrancar,
si ADMIN_EMAIL y ADMIN_PASSWORD estan definidos:

  - no existe esa cuenta      -> se crea con rol admin (contraseña en bcrypt)
  - existe y es admin         -> no se toca (arrancar dos veces no duplica)
  - existe y es jugador       -> error: nunca se le sube el rol a una cuenta ajena

El admin entra por la CLI -> Admin, y Admin valida sus credenciales contra
POST /auth/login de Usuarios (solo deja pasar rol admin).
"""
import logging

from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.config import settings
from app.core.security import hash_password
from app.models import ROLE_ADMIN, User
from app.services.auth import buscar_por_email, normalizar_email

logger = logging.getLogger("usuarios.admin")


class AdminIncompatible(RuntimeError):
    """El correo de ADMIN_EMAIL ya pertenece a una cuenta que no es admin."""


def aprovisionar_admin(db: Session, *, email: str, nombre: str, contrasena: str) -> bool:
    """Crea el admin si falta. Devuelve True si lo creo."""
    correo = normalizar_email(email)
    existente = buscar_por_email(db, correo)
    if existente is not None:
        if existente.role == ROLE_ADMIN:
            return False
        raise AdminIncompatible("ADMIN_EMAIL ya pertenece a una cuenta de jugador")

    db.add(User(email=correo, name=nombre.strip() or "Administrador",
                password_hash=hash_password(contrasena), role=ROLE_ADMIN))
    try:
        db.commit()
    except IntegrityError:
        # Otra instancia lo creo al mismo tiempo: vale solo si quedo como admin.
        db.rollback()
        existente = buscar_por_email(db, correo)
        if existente is not None and existente.role == ROLE_ADMIN:
            return False
        raise
    return True


def aprovisionar_admin_desde_config(db: Session) -> None:
    if not settings.admin_email.strip() or not settings.admin_password:
        logger.warning("ADMIN_EMAIL o ADMIN_PASSWORD sin definir: no se crea el administrador.")
        return
    if aprovisionar_admin(db, email=settings.admin_email, nombre=settings.admin_name,
                          contrasena=settings.admin_password):
        logger.info("Administrador inicial creado (%s).", normalizar_email(settings.admin_email))
