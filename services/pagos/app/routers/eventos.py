"""HU2-06 (parte Pagos): rutas internas de eventos e inscripcion al torneo.

Solo para servicios (header X-Internal-Service-Token):
  POST /internal/events                                 crea el evento al arrancar la simulacion (HU2-07)
  GET  /internal/events/{event_id}                      estados, botin e inscritos
  POST /internal/events/{event_id}/registrations        Orquestador: inscribe y cobra el buy_in (Idempotency-Key)
  GET  /internal/events/{event_id}/registrations/{uid}  Orquestador: ¿esta inscrito? (404 = no)
  POST /internal/events/{event_id}/start                Motor: largada; cierra inscripciones y devuelve si faltan jugadores

El rol (player/admin) lo valida el Orquestador con X-User-Rol; Pagos no.
"""
import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Response, status

from app.core.config import settings
from app.deps import DbSession, ServiceToken, verificar_servicio
from app.models import TOURNAMENT_CANCELADO, Event, EventEntry
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
    SaldoInsuficiente,
    clave_inscripcion,
    crear_evento,
    iniciar_evento,
    inscribir,
    obtener_evento,
    obtener_inscripcion,
)

router = APIRouter(prefix="/internal/events", tags=["eventos"])

NO_EXISTE = "El evento no existe"


def _evento_out(evento: Event, inscritos: int) -> EventOut:
    return EventOut(
        event_id=evento.id,
        race_key=evento.race_key,
        status=evento.status,
        tournament_status=evento.tournament_status,
        buy_in=evento.buy_in,
        initial_chips=evento.initial_chips,
        commission_pct=evento.commission_pct,
        pot=evento.pot,
        registered=inscritos,
        created_at=evento.created_at,
        starts_at=evento.starts_at,
        ended_at=evento.ended_at,
    )


def _inscripcion_out(evento: Event, i: EventEntry, saldo: int | None = None, nueva: bool = False) -> RegistrationOut:
    return RegistrationOut(
        event_id=evento.id,
        user_id=i.user_id,
        tournament_status=evento.tournament_status,
        refunded=evento.tournament_status == TOURNAMENT_CANCELADO,
        buy_in=evento.buy_in,
        initial_chips=evento.initial_chips,
        pot=evento.pot,
        saldo=saldo,
        nueva=nueva,
    )


@router.post(
    "",
    response_model=EventOut,
    status_code=status.HTTP_201_CREATED,
    responses={200: {"description": "El evento ya existía con la misma configuración"}, 409: {"description": "Id ya usado con otra configuración"}},
)
def crear(datos: EventCreateIn, db: DbSession, response: Response, service_token: ServiceToken = None) -> EventOut:
    verificar_servicio(service_token)
    try:
        r = crear_evento(
            db,
            DatosEvento(
                event_id=datos.event_id,
                race_key=datos.race_key.strip(),
                buy_in=datos.buy_in if datos.buy_in is not None else settings.event_buy_in,
                initial_chips=datos.initial_chips if datos.initial_chips is not None else settings.event_initial_chips,
                commission_pct=datos.commission_pct if datos.commission_pct is not None else settings.event_commission_pct,
            ),
        )
    except EventoConflicto:
        raise HTTPException(status.HTTP_409_CONFLICT, "Ese id de evento ya existe con otra configuración") from None
    if not r.nuevo:
        response.status_code = status.HTTP_200_OK
    return _evento_out(r.evento, r.inscritos)


@router.get("/{event_id}", response_model=EventOut, responses={404: {"description": NO_EXISTE}})
def consultar(event_id: str, db: DbSession, service_token: ServiceToken = None) -> EventOut:
    verificar_servicio(service_token)
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
        404: {"description": "Evento inexistente"},
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
    verificar_servicio(service_token)
    if idempotency_key != clave_inscripcion(event_id, datos.user_id):
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Idempotency-Key inválida")
    try:
        r = inscribir(db, event_id, datos.user_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    except InscripcionCerrada:
        raise HTTPException(status.HTTP_409_CONFLICT, "La carrera ya empezó: las inscripciones están cerradas") from None
    except SaldoInsuficiente:
        raise HTTPException(status.HTTP_422_UNPROCESSABLE_ENTITY, "Saldo insuficiente para la inscripción") from None
    if not r.nueva:
        response.status_code = status.HTTP_200_OK
    return _inscripcion_out(r.evento, r.inscripcion, r.saldo, r.nueva)


@router.get(
    "/{event_id}/registrations/{user_id}",
    response_model=RegistrationOut,
    responses={404: {"description": "Evento inexistente o jugador no inscrito"}},
)
def consultar_inscripcion(event_id: str, user_id: uuid.UUID, db: DbSession, service_token: ServiceToken = None) -> RegistrationOut:
    verificar_servicio(service_token)
    try:
        evento, i = obtener_inscripcion(db, event_id, user_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    if i is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "El jugador no está inscrito")
    return _inscripcion_out(evento, i)


@router.post("/{event_id}/start", response_model=EventStartOut, responses={404: {"description": NO_EXISTE}})
def largar(event_id: str, db: DbSession, service_token: ServiceToken = None) -> EventStartOut:
    verificar_servicio(service_token)
    try:
        r = iniciar_evento(db, event_id)
    except EventoNoExiste:
        raise HTTPException(status.HTTP_404_NOT_FOUND, NO_EXISTE) from None
    return EventStartOut(
        event_id=r.evento.id,
        status=r.evento.status,
        tournament_status=r.evento.tournament_status,
        pot=r.evento.pot,
        initial_chips=r.evento.initial_chips,
        players=r.jugadores,
        refunded=r.devueltas,
        nueva=r.nueva,
    )
