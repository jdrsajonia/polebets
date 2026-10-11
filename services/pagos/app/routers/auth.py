"""Endpoints de autenticacion.

  - POST /auth/register : HU-01 registro de jugador.
  - POST /auth/login    : HU-02 (pendiente).
"""
from fastapi import APIRouter, status

from app.deps import DbSession
from app.schemas.auth import RegisterRequest, RegisterResponse
from app.services.auth import registrar_usuario

router = APIRouter(prefix="/auth", tags=["auth"])


@router.post(
    "/register",
    status_code=status.HTTP_201_CREATED,
    response_model=RegisterResponse,
    summary="Registrar un jugador (HU-01)",
)
def register(data: RegisterRequest, db: DbSession) -> RegisterResponse:
    user = registrar_usuario(db, data)
    return RegisterResponse.model_validate(user)