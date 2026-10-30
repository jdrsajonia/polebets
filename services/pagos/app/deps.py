"""Dependencias reutilizables de FastAPI.

En el prototipo 2 Pagos solo recibe llamadas de otros servicios (Orquestador y
Motor), nunca del navegador: no lee cookies ni JWT. Cada ruta /internal/*
exige el token de servicio.

Uso en un router:

    from app.deps import DbSession, ServicioInterno

    router = APIRouter(prefix="/internal", dependencies=[ServicioInterno])
"""
from typing import Annotated

from fastapi import Depends, Header, HTTPException, status
from sqlalchemy.orm import Session

from app.core.internal_auth import token_servicio_valido
from app.db import get_db

DbSession = Annotated[Session, Depends(get_db)]


def verificar_servicio(
    token: Annotated[str | None, Header(alias="X-Internal-Service-Token")] = None,
) -> None:
    """401 si falta el token de servicio o no coincide con INTERNAL_API_TOKEN."""
    if not token_servicio_valido(token):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Servicio no autorizado")


ServicioInterno = Depends(verificar_servicio)
