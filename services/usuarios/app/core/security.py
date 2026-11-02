"""Hash de contraseñas y JWT de sesion (HU2-02, RNF-06).

Usuarios es el unico que EMITE el JWT. Lo validan:

  - Usuarios mismo, en GET /internal/auth, que es la subpeticion auth_request
    del Gateway en cada ruta protegida (T2-01).
  - El Motor, solo mientras el WebSocket no pase por el Gateway: hoy todavia
    verifica la firma por su cuenta (services/motor/internal/auth/contrato.go).

Contrato (el mismo del prototipo 1, para no romper al Motor):

  - Algoritmo HS256 con el secreto compartido JWT_SECRET.
  - Viaja en la cookie HttpOnly "session".
  - Claims: "sub" = id del usuario (UUID en texto), "rol" = "player" | "admin",
    "exp" obligatorio.
"""
import uuid
from datetime import datetime, timedelta, timezone

import bcrypt
import jwt

from app.config import settings
from app.models import ROLE_ADMIN, ROLE_PLAYER

CLAIM_USER_ID = "sub"
CLAIM_ROLE = "rol"
VALID_ROLES = {ROLE_PLAYER, ROLE_ADMIN}


class InvalidTokenError(Exception):
    """El token esta vencido, mal firmado o no cumple el contrato."""


# ---------------------------------------------------------------------------
# Contraseñas
# ---------------------------------------------------------------------------
def hash_password(password: str) -> str:
    """Hash bcrypt con sal propia. La contraseña nunca se guarda en texto plano."""
    return bcrypt.hashpw(password.encode("utf-8"), bcrypt.gensalt()).decode("utf-8")


def verify_password(password: str, password_hash: str) -> bool:
    try:
        return bcrypt.checkpw(password.encode("utf-8"), password_hash.encode("utf-8"))
    except ValueError:
        # Hash con formato invalido en la BD: se trata como clave incorrecta.
        return False


# ---------------------------------------------------------------------------
# JWT
# ---------------------------------------------------------------------------
def create_access_token(user_id: uuid.UUID | str, role: str) -> str:
    if role not in VALID_ROLES:
        raise ValueError(f"rol desconocido: {role!r}")
    now = datetime.now(timezone.utc)
    payload = {
        CLAIM_USER_ID: str(user_id),
        CLAIM_ROLE: role,
        "iat": now,
        "exp": now + timedelta(minutes=settings.jwt_expire_minutes),
    }
    return jwt.encode(payload, settings.jwt_secret, algorithm=settings.jwt_algorithm)


def decode_access_token(token: str) -> tuple[uuid.UUID, str]:
    """Valida firma, vencimiento y claims. Devuelve (user_id, rol)."""
    try:
        claims = jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=[settings.jwt_algorithm],  # fija el algoritmo: rechaza "none"
            options={"require": ["exp", CLAIM_USER_ID, CLAIM_ROLE]},
        )
        user_id = uuid.UUID(claims[CLAIM_USER_ID])
    except (jwt.PyJWTError, ValueError, TypeError) as exc:
        raise InvalidTokenError(str(exc)) from exc

    role = claims[CLAIM_ROLE]
    if role not in VALID_ROLES:
        raise InvalidTokenError(f"rol desconocido: {role!r}")
    return user_id, role
