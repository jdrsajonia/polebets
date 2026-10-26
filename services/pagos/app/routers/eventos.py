"""HU2-06: rutas internas de eventos multijugador e inscripciones.

Solo para servicios (header X-Internal-Service-Token):
  POST /internal/events                                 Motor: crea el evento al arrancar la simulacion
  GET  /internal/events/{event_id}                      estado, botin e inscritos
  POST /internal/events/{event_id}/registrations        Orquestador: inscribe y cobra (Idempotency-Key)
  GET  /internal/events/{event_id}/registrations/{uid}  Orquestador: ¿esta inscrito? (decide el modo)
  POST /internal/events/{event_id}/start                Motor: largada; cierra inscripciones y devuelve si faltan jugadores
"""
import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Response, status

from app.core.config import settings
from app.deps import DbSession
from app.models import Event
from app.routers.internal import _verificar_servicio
from app.schemas.eventos import (
    EventCreateIn,
    EventOut,
    EventStartOut,
    RegistrationIn,
    RegistrationOut,
)
from app.services.eventos import (
    DatosEvento,
    EventoConflicto,
    EventoNoExiste,
    InscripcionCerrada,
    NoEsJugador,
    SaldoInsuficiente,
    UsuarioNoExiste,
    clave_inscripcion,
    crear_evento,
    iniciar_evento,
    inscribir,
    obtener_evento,
    obtener_inscripcion,
)

router = APIRouter(prefix="/internal/events", tags=["eventos"])

ServiceToken = Annotated[str | None, Header(alias="X-Internal-Service-Token")]
NO_EXISTE = "El evento no existe"


def _evento_out(evento: Event, inscritos: int) -> EventOut:
    return EventOut(
        event_id=evento.id,
        name=evento.name,
        race_ref=evento.race_ref,
        status=evento.status,
        entry_fee=evento.entry_fee,
        initial_chips=evento.initial_chips,
        commission_pct=evento.commission_pct,
        min_wager_pct=evento.min_wager_pct,
        min_players=evento.min_players,
        pot=evento.pot,
        registered=inscritos,
        created_at=evento.created_at,
        started_at=evento.started_at,
    )


@router.post(
    "",
    response_model=EventOut,
    status_code=status.HTTP_201_CREATED,
    responses={200: {"description": "El evento ya existia con la misma configuracion"}, 409: {"description": "Id ya usado con otra configuracion"}},
)
def crear(datos: EventCreateIn, db: DbSession, response: Response, service_token: ServiceToken = None) -> EventOut:
    _verificar_servicio(service_token)
    def valor(x, defecto):
        return defecto if x is None else x

    try:
        r = crear_evento(
            db,
            DatosEvento(
                event_id=datos.event_id,
                name=datos.name.strip(),
                race_ref=datos.race_ref.strip(),
                entry_fee=valor(datos.entry_fee, settings.event_entry_fee),
                initial_chips=valor(datos.initial_chips, settings.event_initial_chips),
                commission_pct=valor(datos.commission_pct, settings.event_commission_pct),
                min_wager_pct=valor(datos.min_wager_pct, settings.event_min_wager_pct),
                min_players=valor(datos.min_players, settings.event_min_players),
            ),
        )
    except EventoConflicto:
        raise HTTPException(status.HTTP_409_CONFLICT, "Ese id de evento ya existe con otra configuración") from None
    if not r.nuevo:
        response.status_code = status.HTTP_200_OK
    return _evento_out(r.evento, r.inscritos)


@router.get("/{event_id}", response_model=EventOut, responses={404: {"description": NO_EXISTE}})
def consultar(event_id: str, db: DbSession, service_token: ServiceToken = None) -> EventOut:
    _verificar_servicio(service_token)
    try:
        r = obtener_evento(db, event_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    return _evento_out(r.evento, r.inscritos)


@router.post(
    "/{event_id}/registrations",
    response_model=RegistrationOut,
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {"description": "Ya estaba inscrito: no se cobra de nuevo"},
        400: {"description": "Idempotency-Key inválida"},
        403: {"description": "Solo los jugadores se inscriben"},
        404: {"description": "Evento o usuario inexistente"},
        409: {"description": "La carrera ya largó: inscripciones cerradas"},
        422: {"description": "Saldo insuficiente"},
    },
)
def inscribirse(
    event_id: str,
    datos: RegistrationIn,
    db: DbSession,
    response: Response,
    service_token: ServiceToken = None,
    idempotency_key: Annotated[str | None, Header(alias="Idempotency-Key")] = None,
) -> RegistrationOut:
    _verificar_servicio(service_token)
    if idempotency_key != clave_inscripcion(event_id, datos.user_id):
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Idempotency-Key inválida")
    try:
        r = inscribir(db, event_id, datos.user_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    except UsuarioNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Usuario inexistente") from None
    except NoEsJugador:
        raise HTTPException(status.HTTP_403_FORBIDDEN, "Solo los jugadores pueden inscribirse") from None
    except InscripcionCerrada:
        raise HTTPException(status.HTTP_409_CONFLICT, "La carrera ya empezó: las inscripciones están cerradas") from None
    except SaldoInsuficiente:
        raise HTTPException(status.HTTP_422_UNPROCESSABLE_ENTITY, "Saldo insuficiente para la inscripción") from None
    if not r.nueva:
        response.status_code = status.HTTP_200_OK
    return RegistrationOut(
        event_id=event_id,
        user_id=r.inscripcion.user_id,
        status=r.inscripcion.status,
        fee=r.inscripcion.fee,
        initial_chips=r.evento.initial_chips,
        pot=r.evento.pot,
        saldo=r.saldo,
        nueva=r.nueva,
    )


@router.get(
    "/{event_id}/registrations/{user_id}",
    response_model=RegistrationOut,
    responses={404: {"description": "Evento inexistente o jugador no inscrito"}},
)
def consultar_inscripcion(event_id: str, user_id: uuid.UUID, db: DbSession, service_token: ServiceToken = None) -> RegistrationOut:
    _verificar_servicio(service_token)
    try:
        i = obtener_inscripcion(db, event_id, user_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    if i is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "El jugador no está inscrito")
    evento = db.get(Event, event_id)
    return RegistrationOut(
        event_id=event_id, user_id=i.user_id, status=i.status, fee=i.fee, initial_chips=evento.initial_chips, pot=evento.pot
    )


@router.post("/{event_id}/start", response_model=EventStartOut, responses={404: {"description": NO_EXISTE}})
def largar(event_id: str, db: DbSession, service_token: ServiceToken = None) -> EventStartOut:
    _verificar_servicio(service_token)
    try:
        r = iniciar_evento(db, event_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    return EventStartOut(
        event_id=r.evento.id,
        status=r.evento.status,
        pot=r.evento.pot,
        initial_chips=r.evento.initial_chips,
        players=r.jugadores,
        refunded=r.devueltas,
        nueva=r.nueva,
    )
