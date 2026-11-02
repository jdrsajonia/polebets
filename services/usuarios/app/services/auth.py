"""Inicio de sesion (HU2-02).

El login queda detras de la interfaz `ProveedorIdentidad`: el router solo
conoce esa interfaz. Hoy la implementa `ProveedorLocal` (correo + contraseña
contra PostgreSQL Usuarios); para pasar a OAuth basta con otra implementacion
y cambiar `get_proveedor_identidad`. Lo que no cambia: despues de identificar
al usuario, Usuarios emite su propio JWT y el Gateway lo valida igual.
"""
from functools import lru_cache
from typing import Protocol

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.core.security import hash_password, verify_password
from app.models import User


def normalizar_email(email: str) -> str:
    """Forma canonica del correo: sin espacios y en minusculas.

    El registro guarda el correo con esta misma funcion, asi "Ana@Mail.com" y
    "ana@mail.com" son la misma cuenta.
    """
    return email.strip().lower()


def buscar_por_email(db: Session, email: str) -> User | None:
    # func.lower protege tambien a cuentas guardadas antes de normalizar.
    return db.scalar(select(User).where(func.lower(User.email) == normalizar_email(email)))


class ProveedorIdentidad(Protocol):
    """Interfaz de autenticacion: dice QUIEN es el usuario, nada mas."""

    def autenticar(self, db: Session, email: str, password: str) -> User | None: ...


@lru_cache
def _hash_senuelo() -> str:
    # Se calcula una sola vez, la primera vez que alguien prueba un correo inexistente.
    return hash_password("contraseña-señuelo-que-nadie-usa")


class ProveedorLocal:
    """Login propio: correo + contraseña con hash bcrypt (RNF-06)."""

    def autenticar(self, db: Session, email: str, password: str) -> User | None:
        user = buscar_por_email(db, email)
        if user is None:
            # Se compara igual contra un hash señuelo para que "no existe ese
            # correo" tarde lo mismo que "clave incorrecta". Sin esto, midiendo
            # tiempos se podria averiguar que correos estan registrados.
            verify_password(password, _hash_senuelo())
            return None
        if not verify_password(password, user.password_hash):
            return None
        return user


def get_proveedor_identidad() -> ProveedorIdentidad:
    """Dependencia de FastAPI. Para migrar a OAuth se cambia solo esta funcion."""
    return ProveedorLocal()
