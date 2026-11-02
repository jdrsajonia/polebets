"""Rutas internas de Usuarios. Ninguna se publica: el Gateway no las enruta hacia afuera.

router_gateway (sin token de servicio):
    GET /internal/auth   subpeticion auth_request del Gateway (T2-01, RNF-17)

    Nginx no puede leer el .env para mandar el token de servicio, y esta ruta
    no revela nada que el dueño de la cookie no tenga ya. Usuarios no esta
    publicado al host (solo vive en la red interna), asi que solo el Gateway
    la alcanza.

router_servicio (exigen X-Internal-Service-Token, las llama el Orquestador):
    POST /internal/users        registro orquestado, paso 2 (HU2-01)
    GET  /internal/users/{id}   perfil para la pagina SSR (HU2-03)
"""
import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Request, Response, status

from app.core.session import sin_cache
from app.deps import DbSession, ServicioInterno, usuario_de_cookie
from app.schemas.usuarios import CrearUsuarioIn, UsuarioOut
from app.services.usuarios import MENSAJE_CORREO_REGISTRADO, CorreoRegistrado, crear_usuario, obtener_usuario

# Headers que el Gateway copia a la peticion original (auth_request_set).
HEADER_USUARIO = "X-User-Id"
HEADER_ROL = "X-User-Rol"

router_gateway = APIRouter(prefix="/internal", tags=["gateway"])
router_servicio = APIRouter(prefix="/internal", tags=["internal"], dependencies=[ServicioInterno])


# ---------------------------------------------------------------------------
# HU2-02: validacion de sesion para el Gateway
# ---------------------------------------------------------------------------
@router_gateway.get(
    "/auth",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={204: {"description": "Sesion valida: headers X-User-Id y X-User-Rol"}, 401: {"description": "Sin sesion"}},
)
def validar_sesion(request: Request, response: Response, db: DbSession) -> None:
    """204 + X-User-Id y X-User-Rol si la cookie es valida; 401 si no.

    auth_request solo mira el codigo: 2xx deja pasar, 401 corta la peticion.
    El rol sale de la BD (no del token): un usuario borrado o con otro rol
    queda reflejado en la siguiente peticion.
    """
    user = usuario_de_cookie(request, db)
    if user is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="No hay sesión")
    response.headers[HEADER_USUARIO] = str(user.id)
    response.headers[HEADER_ROL] = user.role
    sin_cache(response)


# ---------------------------------------------------------------------------
# HU2-01: registro orquestado (paso 2)
# ---------------------------------------------------------------------------
@router_servicio.post(
    "/users",
    response_model=UsuarioOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Reintento: el id ya existia con el mismo correo; no se cambia nada"},
        400: {"description": "Idempotency-Key distinta de user:{id}"},
        401: {"description": "Token de servicio ausente o inválido"},
        409: {"description": MENSAJE_CORREO_REGISTRADO},
        422: {"description": "Datos invalidos (correo mal formado, contraseña corta...)"},
    },
)
def crear(
    datos: CrearUsuarioIn,
    db: DbSession,
    response: Response,
    idempotency_key: Annotated[str | None, Header(alias="Idempotency-Key")] = None,
) -> UsuarioOut:
    """Crea el jugador con el id del Orquestador.

    La clave `user:{id}` es opcional para Usuarios (la operacion ya es
    idempotente por el id), pero si llega debe coincidir: el Orquestador la
    manda para que su cliente interno pueda reintentar este POST.
    """
    if idempotency_key is not None and idempotency_key != f"user:{datos.id}":
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="Idempotency-Key inválida")
    try:
        user, nuevo = crear_usuario(db, datos)
    except CorreoRegistrado:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=MENSAJE_CORREO_REGISTRADO) from None
    if not nuevo:
        response.status_code = status.HTTP_200_OK
    return UsuarioOut.desde_modelo(user)


@router_servicio.get("/users/{user_id}", response_model=UsuarioOut, responses={404: {"description": "No existe"}})
def ver(user_id: uuid.UUID, db: DbSession) -> UsuarioOut:
    user = obtener_usuario(db, user_id)
    if user is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Usuario inexistente")
    return UsuarioOut.desde_modelo(user)
