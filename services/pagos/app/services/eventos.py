"""HU2-06: eventos multijugador e inscripciones.

Reglas (decisiones del equipo para el prototipo 2):
  - El Motor crea el evento cuando el admin arranca la simulacion.
  - Inscribirse cuesta entry_fee PoleCoins (minimo 100). El pago va al botin.
  - Una sola inscripcion por jugador y evento; los reintentos no cobran doble.
  - Solo se puede inscribir antes de la largada. La largada la marca el Motor
    con el TIEMPO DE CARRERA, no con la hora real.
  - Al largar, con menos de min_players inscritos se devuelven todas las
    inscripciones (botin a 0) y el evento queda solo en modo individual.
  - Las fichas (initial_chips) son de juego y las maneja el Motor en Redis.

Concurrencia: cada operacion bloquea la fila del evento (SELECT ... FOR
UPDATE). Asi una inscripcion y la largada nunca se cruzan, y el botin se
actualiza sin perder sumas.

T2-03: el saldo se lee y escribe solo en _debitar_saldo y _acreditar_saldo.
Cuando el saldo pase a la tabla wallets, solo cambian esas dos funciones.
"""
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone

from sqlalchemy import func, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.models import (
    EVENT_INDIVIDUAL_ONLY,
    EVENT_MULTIPLAYER,
    EVENT_REGISTRATION,
    LEDGER_COMPLETED,
    LEDGER_EVENT_ENTRY,
    LEDGER_EVENT_REFUND,
    REGISTRATION_ACTIVE,
    REGISTRATION_REFUNDED,
    ROLE_PLAYER,
    Event,
    EventRegistration,
    LedgerEntry,
    User,
)


class EventoNoExiste(Exception):
    pass


class EventoConflicto(Exception):
    """El id del evento ya existe con otra configuracion."""


class InscripcionCerrada(Exception):
    """La carrera ya largo: no se aceptan inscripciones."""


class UsuarioNoExiste(Exception):
    pass


class SaldoInsuficiente(Exception):
    pass


class NoEsJugador(Exception):
    pass


def clave_inscripcion(event_id: str, user_id: uuid.UUID) -> str:
    return f"event-entry:{event_id}:{user_id}"


def clave_devolucion(event_id: str, user_id: uuid.UUID) -> str:
    return f"event-refund:{event_id}:{user_id}"


# ---------------------------------------------------------------------------
# Saldo (unico punto que cambia con T2-03)
# ---------------------------------------------------------------------------
def _debitar_saldo(db: Session, user_id: uuid.UUID, monto: int) -> int | None:
    """Resta el monto si alcanza. Devuelve el saldo nuevo o None si no alcanza."""
    return db.execute(
        update(User)
        .where(User.id == user_id, User.balance >= monto)
        .values(balance=User.balance - monto)
        .returning(User.balance)
    ).scalar_one_or_none()


def _acreditar_saldo(db: Session, user_id: uuid.UUID, monto: int) -> int:
    return db.execute(
        update(User).where(User.id == user_id).values(balance=User.balance + monto).returning(User.balance)
    ).scalar_one()


# ---------------------------------------------------------------------------
# Crear evento (lo llama el Motor al arrancar la simulacion)
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class DatosEvento:
    event_id: str
    name: str
    race_ref: str
    entry_fee: int
    initial_chips: int
    commission_pct: int
    min_wager_pct: int
    min_players: int


@dataclass(frozen=True)
class ResultadoEvento:
    evento: Event
    inscritos: int
    nuevo: bool


def _mismo_evento(e: Event, d: DatosEvento) -> bool:
    return (
        e.name == d.name
        and e.race_ref == d.race_ref
        and e.entry_fee == d.entry_fee
        and e.initial_chips == d.initial_chips
        and e.commission_pct == d.commission_pct
        and e.min_wager_pct == d.min_wager_pct
        and e.min_players == d.min_players
    )


def _contar_inscritos(db: Session, event_id: str) -> int:
    return db.scalar(
        select(func.count())
        .select_from(EventRegistration)
        .where(EventRegistration.event_id == event_id, EventRegistration.status == REGISTRATION_ACTIVE)
    )


def crear_evento(db: Session, datos: DatosEvento) -> ResultadoEvento:
    """Idempotente por id: el mismo evento otra vez devuelve el existente."""
    existente = db.get(Event, datos.event_id)
    if existente is not None:
        if not _mismo_evento(existente, datos):
            raise EventoConflicto()
        return ResultadoEvento(existente, _contar_inscritos(db, existente.id), nuevo=False)

    evento = Event(
        id=datos.event_id,
        name=datos.name,
        race_ref=datos.race_ref,
        status=EVENT_REGISTRATION,
        entry_fee=datos.entry_fee,
        initial_chips=datos.initial_chips,
        commission_pct=datos.commission_pct,
        min_wager_pct=datos.min_wager_pct,
        min_players=datos.min_players,
        pot=0,
    )
    db.add(evento)
    try:
        db.commit()
    except IntegrityError:
        # Dos creaciones simultaneas del mismo id: gana la primera.
        db.rollback()
        return crear_evento(db, datos)
    db.refresh(evento)
    return ResultadoEvento(evento, 0, nuevo=True)


def obtener_evento(db: Session, event_id: str) -> ResultadoEvento:
    evento = db.get(Event, event_id)
    if evento is None:
        raise EventoNoExiste()
    return ResultadoEvento(evento, _contar_inscritos(db, event_id), nuevo=False)


# ---------------------------------------------------------------------------
# Inscribirse (lo llama el Orquestador)
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class ResultadoInscripcion:
    inscripcion: EventRegistration
    evento: Event
    saldo: int
    nueva: bool


def _saldo(db: Session, user_id: uuid.UUID) -> int:
    return db.scalar(select(User.balance).where(User.id == user_id))


def inscribir(db: Session, event_id: str, user_id: uuid.UUID) -> ResultadoInscripcion:
    # 1. Bloquear el evento: serializa inscripciones y largada del mismo evento.
    evento = db.scalar(select(Event).where(Event.id == event_id).with_for_update())
    if evento is None:
        raise EventoNoExiste()

    # 2. Ya inscrito: se devuelve lo que habia (una por evento, sin cobrar doble).
    previa = db.scalar(
        select(EventRegistration).where(
            EventRegistration.event_id == event_id, EventRegistration.user_id == user_id
        )
    )
    if previa is not None:
        db.commit()  # suelta el bloqueo
        return ResultadoInscripcion(previa, evento, _saldo(db, user_id), nueva=False)

    if evento.status != EVENT_REGISTRATION:
        db.rollback()
        raise InscripcionCerrada()

    usuario = db.get(User, user_id)
    if usuario is None:
        db.rollback()
        raise UsuarioNoExiste()
    if usuario.role != ROLE_PLAYER:
        db.rollback()
        raise NoEsJugador()

    # 3. Cobrar, sumar al botin, registrar la inscripcion y el movimiento: todo
    #    en una transaccion.
    saldo = _debitar_saldo(db, user_id, evento.entry_fee)
    if saldo is None:
        db.rollback()
        raise SaldoInsuficiente()

    evento.pot += evento.entry_fee
    inscripcion = EventRegistration(event_id=event_id, user_id=user_id, fee=evento.entry_fee, status=REGISTRATION_ACTIVE)
    db.add(inscripcion)
    db.add(
        LedgerEntry(
            user_id=user_id,
            event_id=event_id,
            type=LEDGER_EVENT_ENTRY,
            tokens_delta=-evento.entry_fee,
            status=LEDGER_COMPLETED,
            idempotency_key=clave_inscripcion(event_id, user_id),
        )
    )
    db.commit()
    db.refresh(inscripcion)
    db.refresh(evento)
    return ResultadoInscripcion(inscripcion, evento, saldo, nueva=True)


def obtener_inscripcion(db: Session, event_id: str, user_id: uuid.UUID) -> EventRegistration | None:
    if db.get(Event, event_id) is None:
        raise EventoNoExiste()
    return db.scalar(
        select(EventRegistration).where(
            EventRegistration.event_id == event_id, EventRegistration.user_id == user_id
        )
    )


# ---------------------------------------------------------------------------
# Largada (la marca el Motor con el tiempo de carrera)
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class ResultadoLargada:
    evento: Event
    jugadores: list[uuid.UUID]  # inscritos activos: el Motor les crea las fichas
    devueltas: int
    nueva: bool


def iniciar_evento(db: Session, event_id: str) -> ResultadoLargada:
    """Cierra inscripciones. Idempotente: repetirla devuelve el mismo resultado."""
    evento = db.scalar(select(Event).where(Event.id == event_id).with_for_update())
    if evento is None:
        raise EventoNoExiste()

    inscripciones = list(
        db.scalars(select(EventRegistration).where(EventRegistration.event_id == event_id).order_by(EventRegistration.created_at))
    )

    if evento.status != EVENT_REGISTRATION:
        db.commit()
        activos = [i.user_id for i in inscripciones if i.status == REGISTRATION_ACTIVE]
        devueltas = sum(1 for i in inscripciones if i.status == REGISTRATION_REFUNDED)
        return ResultadoLargada(evento, activos, devueltas, nueva=False)

    activas = [i for i in inscripciones if i.status == REGISTRATION_ACTIVE]
    evento.started_at = datetime.now(timezone.utc)

    if len(activas) < evento.min_players:
        # Pocos inscritos: no hay multijugador. Se devuelve todo.
        for i in activas:
            _acreditar_saldo(db, i.user_id, i.fee)
            i.status = REGISTRATION_REFUNDED
            db.add(
                LedgerEntry(
                    user_id=i.user_id,
                    event_id=event_id,
                    type=LEDGER_EVENT_REFUND,
                    tokens_delta=i.fee,
                    status=LEDGER_COMPLETED,
                    idempotency_key=clave_devolucion(event_id, i.user_id),
                )
            )
        evento.pot = 0
        evento.status = EVENT_INDIVIDUAL_ONLY
        db.commit()
        db.refresh(evento)
        return ResultadoLargada(evento, [], len(activas), nueva=True)

    evento.status = EVENT_MULTIPLAYER
    db.commit()
    db.refresh(evento)
    return ResultadoLargada(evento, [i.user_id for i in activas], 0, nueva=True)
