"""Dependencias de FastAPI compartidas por los routers.

    DbSession        sesion de SQLAlchemy por request
    ServicioInterno  exige X-Internal-Service-Token (rutas /internal/* del Orquestador)
    usuario_de_cookie  el dueño de la cookie de sesion, o None
"""
from typing import Annotated

from fastapi import Depends, Header, HTTPException, Request, status
from sqlalchemy.orm import Session

from app.config import settings
from app.core.internal_auth import token_servicio_valido
from app.core.security import InvalidTokenError, decode_access_token
from app.db import get_db
from app.models import User

DbSession = Annotated[Session, Depends(get_db)]


def verificar_servicio(
    token: Annotated[str | None, Header(alias="X-Internal-Service-Token")] = None,
) -> None:
    if not token_servicio_valido(token):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Token de servicio inválido")


ServicioInterno = Depends(verificar_servicio)


def usuario_de_cookie(request: Request, db: Session) -> User | None:
    """Lee la cookie de sesion y devuelve el usuario, o None si no hay sesion valida.

    Ademas de validar el JWT, busca al usuario en la BD: si lo borraron, la
    sesion deja de servir aunque el token no haya vencido. El ROL se toma de la
    BD y no del token, asi un cambio de rol aplica de inmediato.
    """
    token = request.cookies.get(settings.session_cookie_name)
    if not token:
        return None
    try:
        user_id, _rol_del_token = decode_access_token(token)
    except InvalidTokenError:
        return None
    return db.get(User, user_id)
