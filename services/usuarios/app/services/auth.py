"""Inicio de sesion (HU2-02) y bloqueo progresivo tras fallos (Restrict login, BASS 11.2).

El login queda detras de la interfaz `ProveedorIdentidad`: el router solo
conoce esa interfaz. Hoy la implementa `ProveedorLocal` (correo + contraseña
contra PostgreSQL Usuarios); para pasar a OAuth basta con otra implementacion
y cambiar `get_proveedor_identidad`. Lo que no cambia: despues de identificar
al usuario, Usuarios emite su propio JWT y el Gateway lo valida igual.

Bloqueo progresivo: los primeros INTENTOS_LIBRES fallos seguidos no bloquean
nada (responden 401 normal). Desde el fallo siguiente, la cuenta queda
bloqueada un tiempo creciente (TIEMPOS_BLOQUEO_SEGUNDOS, con tope en el ultimo
valor). Un login correcto reinicia el contador a cero. Esto es independiente
del rate limiting del Gateway (Escenario 5): aqui se cuenta por CUENTA
(correo), no por IP, porque el objetivo es proteger la contraseña de cada
jugador, no el trafico en general.
"""
from datetime import datetime, timedelta, timezone
from functools import lru_cache
from typing import Protocol

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.core.security import hash_password, verify_password
from app.models import User

# Fallos seguidos permitidos antes de que el primer bloqueo entre en juego.
INTENTOS_LIBRES = 3
# Duracion del bloqueo segun cuantos fallos de mas lleve la cuenta; el ultimo
# valor se repite si sigue fallando (no crece sin limite).
TIEMPOS_BLOQUEO_SEGUNDOS = [30, 120, 300]


def normalizar_email(email: str) -> str:
    """Forma canonica del correo: sin espacios y en minusculas.

    El registro guarda el correo con esta misma funcion, asi "Ana@Mail.com" y
    "ana@mail.com" son la misma cuenta.
    """
    return email.strip().lower()


def buscar_por_email(db: Session, email: str) -> User | None:
    # func.lower protege tambien a cuentas guardadas antes de normalizar.
    return db.scalar(select(User).where(func.lower(User.email) == normalizar_email(email)))


class CuentaBloqueada(Exception):
    """La cuenta esta bloqueada por demasiados fallos seguidos."""

    def __init__(self, segundos_restantes: int) -> None:
        self.segundos_restantes = segundos_restantes
        super().__init__(f"Cuenta bloqueada, quedan {segundos_restantes} segundos")


class ProveedorIdentidad(Protocol):
    """Interfaz de autenticacion: dice QUIEN es el usuario, nada mas."""

    def autenticar(self, db: Session, email: str, password: str) -> User | None: ...


@lru_cache
def _hash_senuelo() -> str:
    # Se calcula una sola vez, la primera vez que alguien prueba un correo inexistente.
    return hash_password("contraseña-señuelo-que-nadie-usa")


def _segundos_de_bloqueo(intentos_fallidos: int) -> int:
    """Cuanto dura el bloqueo segun el numero de fallos seguidos ya contados."""
    indice = intentos_fallidos - INTENTOS_LIBRES - 1
    indice = min(max(indice, 0), len(TIEMPOS_BLOQUEO_SEGUNDOS) - 1)
    return TIEMPOS_BLOQUEO_SEGUNDOS[indice]


class ProveedorLocal:
    """Login propio: correo + contraseña con hash bcrypt (RNF-06), con bloqueo progresivo."""

    def autenticar(self, db: Session, email: str, password: str) -> User | None:
        user = buscar_por_email(db, email)
        if user is None:
            # Se compara igual contra un hash señuelo para que "no existe ese
            # correo" tarde lo mismo que "clave incorrecta". Sin esto, midiendo
            # tiempos se podria averiguar que correos estan registrados.
            verify_password(password, _hash_senuelo())
            return None

        ahora = datetime.now(timezone.utc)
        if user.locked_until is not None and user.locked_until > ahora:
            raise CuentaBloqueada(int((user.locked_until - ahora).total_seconds()))

        if not verify_password(password, user.password_hash):
            user.failed_login_attempts += 1
            if user.failed_login_attempts > INTENTOS_LIBRES:
                segundos = _segundos_de_bloqueo(user.failed_login_attempts)
                user.locked_until = ahora + timedelta(seconds=segundos)
            db.commit()
            return None

        if user.failed_login_attempts or user.locked_until:
            user.failed_login_attempts = 0
            user.locked_until = None
            db.commit()
        return user


def get_proveedor_identidad() -> ProveedorIdentidad:
    """Dependencia de FastAPI. Para migrar a OAuth se cambia solo esta funcion."""
    return ProveedorLocal()
