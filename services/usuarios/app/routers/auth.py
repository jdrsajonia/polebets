"""Sesion del jugador (HU2-02). El Gateway enruta aqui /api/auth/* sin auth_request.

    POST /auth/login    correo + contraseña -> cookie HttpOnly "session" con el JWT
    POST /auth/logout   borra la cookie (siempre 204)
    GET  /auth/me       usuario de la cookie, o 401 (el front lo usa al recargar)

El JWT nunca viaja en el cuerpo: solo en la cookie, que JavaScript no puede leer.
"""
from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Request, Response, status

from app.core.security import create_access_token
from app.core.session import borrar_cookie_sesion, poner_cookie_sesion, sin_cache
from app.deps import DbSession, usuario_de_cookie
from app.schemas.usuarios import LoginIn, UsuarioOut
from app.services.auth import ProveedorIdentidad, get_proveedor_identidad

router = APIRouter(prefix="/auth", tags=["auth"])

# Un solo mensaje para cualquier credencial invalida: no revela si el correo
# esta registrado.
MENSAJE_CREDENCIALES = "Correo o contraseña incorrectos"
MENSAJE_SIN_SESION = "No hay sesión"


@router.post("/login", response_model=UsuarioOut, responses={401: {"description": MENSAJE_CREDENCIALES}})
def login(
    datos: LoginIn,
    response: Response,
    db: DbSession,
    proveedor: Annotated[ProveedorIdentidad, Depends(get_proveedor_identidad)],
) -> UsuarioOut:
    """200 con el usuario y la cookie puesta; 401 con el mensaje generico.

    Sirve para jugadores (front) y para el admin (Admin valida aqui las
    credenciales de la CLI y solo deja pasar rol admin, RF-21).
    """
    user = proveedor.autenticar(db, datos.email, datos.password)
    if user is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail=MENSAJE_CREDENCIALES)
    poner_cookie_sesion(response, create_access_token(user.id, user.role))
    sin_cache(response)
    return UsuarioOut.desde_modelo(user)


@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT)
def logout(response: Response) -> None:
    """No exige sesion: con la cookie vencida igual se debe poder salir.

    Tras borrar la cookie, la siguiente peticion protegida no trae sesion y el
    Gateway responde 401 (criterio de HU2-02). El JWT no se guarda en el
    servidor; revocar tokens antes de que venzan queda como deuda tecnica.
    """
    borrar_cookie_sesion(response)
    sin_cache(response)


@router.get("/me", response_model=UsuarioOut, responses={401: {"description": MENSAJE_SIN_SESION}})
def me(request: Request, response: Response, db: DbSession) -> UsuarioOut:
    user = usuario_de_cookie(request, db)
    if user is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail=MENSAJE_SIN_SESION)
    sin_cache(response)
    return UsuarioOut.desde_modelo(user)
