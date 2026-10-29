"""HU2-06 (parte Pagos): eventos e inscripcion al torneo multijugador.

Reglas del prototipo 2:
  - El evento se crea al arrancar la simulacion (HU2-07 decide quien lo llama).
  - Inscribirse cuesta buy_in PoleCoins (100 por defecto). El pago va al botin.
  - Una sola inscripcion por jugador y evento; los reintentos no cobran doble.
  - Solo hay inscripciones antes de la largada. La largada la marca el Motor
    con el TIEMPO DE CARRERA, no con la hora real.
  - Al largar, con menos de EVENT_MIN_PLAYERS inscritos se devuelve la
    inscripcion a todos y el torneo queda cancelado (la carrera sigue).
  - El rol del usuario lo valida el Orquestador, no Pagos.

Concurrencia: inscribir y largar bloquean la fila del evento (SELECT ... FOR
UPDATE); asi no se cruzan y el botin no pierde sumas.

T2-03: el saldo solo se toca en _debitar_saldo y _acreditar_saldo. Cuando
pase a la tabla wallets, solo cambian esas dos funciones.
"""
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone

from sqlalchemy import func, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.core.config import settings
from app.models import (
    EVENT_EN_CURSO,
    EVENT_PROGRAMADO,
    LEDGER_BUY_IN,
    LEDGER_COMPLETED,
    LEDGER_REFUND,
    TOURNAMENT_ABIERTO,
    TOURNAMENT_ACTIVO,
    TOURNAMENT_CANCELADO,
    Event,
    EventEntry,
    LedgerEntry,
    User,
)


class EventoNoExiste(Exception):
    pass


class EventoConflicto(Exception):
    """El id del evento ya existe con otra configuracion."""


class InscripcionCerrada(Exception):
    """La carrera ya largo: no se aceptan inscripciones."""


class SaldoInsuficiente(Exception):
    """No alcanza el saldo, o el usuario no tiene saldo registrado."""


def clave_inscripcion(event_id: str, user_id: uuid.UUID) -> str:
    return f"event-entry:{event_id}:{user_id}"


def clave_devolucion(event_id: str, user_id: uuid.UUID) -> str:
    return f"event-refund:{event_id}:{user_id}"


# ---------------------------------------------------------------------------
# Saldo (unico punto que cambia con T2-03)
# ---------------------------------------------------------------------------
def _debitar_saldo(db: Session, user_id: uuid.UUID, monto: int) -> int | None:
    """Resta el monto si alcanza. None si no alcanza o no hay saldo para ese usuario."""
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


def _saldo(db: Session, user_id: uuid.UUID) -> int | None:
    return db.scalar(select(User.balance).where(User.id == user_id))


def _contar_inscritos(db: Session, event_id: str) -> int:
    """Todas las filas del evento, tambien las de un torneo cancelado (historial)."""
    return db.scalar(select(func.count()).select_from(EventEntry).where(EventEntry.event_id == event_id))


# ---------------------------------------------------------------------------
# Crear y consultar evento
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class DatosEvento:
    event_id: str
    race_key: str
    buy_in: int
    initial_chips: int
    commission_pct: int


@dataclass(frozen=True)
class ResultadoEvento:
    evento: Event
    inscritos: int
    nuevo: bool


def _mismo_evento(e: Event, d: DatosEvento) -> bool:
    return (e.race_key, e.buy_in, e.initial_chips, e.commission_pct) == (
        d.race_key,
        d.buy_in,
        d.initial_chips,
        d.commission_pct,
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
        race_key=datos.race_key,
        status=EVENT_PROGRAMADO,
        tournament_status=TOURNAMENT_ABIERTO,
        buy_in=datos.buy_in,
        initial_chips=datos.initial_chips,
        commission_pct=datos.commission_pct,
        pot=0,
    )
    db.add(evento)
    try:
        db.commit()
    except IntegrityError:
        db.rollback()  # dos creaciones simultaneas del mismo id: gana la primera
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
    inscripcion: EventEntry
    evento: Event
    saldo: int | None
    nueva: bool


def _buscar_inscripcion(db: Session, event_id: str, user_id: uuid.UUID) -> EventEntry | None:
    return db.get(EventEntry, (event_id, user_id))


def inscribir(db: Session, event_id: str, user_id: uuid.UUID) -> ResultadoInscripcion:
    # 1. Bloquear el evento: serializa inscripciones y largada del mismo evento.
    evento = db.scalar(select(Event).where(Event.id == event_id).with_for_update())
    if evento is None:
        raise EventoNoExiste()

    # 2. Ya inscrito: se devuelve lo que habia (una por evento, sin cobrar doble).
    previa = _buscar_inscripcion(db, event_id, user_id)
    if previa is not None:
        db.commit()  # suelta el bloqueo
        return ResultadoInscripcion(previa, evento, _saldo(db, user_id), nueva=False)

    if evento.tournament_status != TOURNAMENT_ABIERTO:
        db.rollback()
        raise InscripcionCerrada()

    # 3. Cobrar, sumar al botin, crear la inscripcion y el movimiento: todo en
    #    una transaccion.
    saldo = _debitar_saldo(db, user_id, evento.buy_in)
    if saldo is None:
        db.rollback()
        raise SaldoInsuficiente()

    evento.pot += evento.buy_in
    inscripcion = EventEntry(event_id=event_id, user_id=user_id)
    db.add(inscripcion)
    db.add(
        LedgerEntry(
            user_id=user_id,
            event_id=event_id,
            type=LEDGER_BUY_IN,
            tokens_delta=-evento.buy_in,
            status=LEDGER_COMPLETED,
            idempotency_key=clave_inscripcion(event_id, user_id),
        )
    )
    db.commit()
    db.refresh(inscripcion)
    db.refresh(evento)
    return ResultadoInscripcion(inscripcion, evento, saldo, nueva=True)


def obtener_inscripcion(db: Session, event_id: str, user_id: uuid.UUID) -> tuple[Event, EventEntry | None]:
    evento = db.get(Event, event_id)
    if evento is None:
        raise EventoNoExiste()
    return evento, _buscar_inscripcion(db, event_id, user_id)


# ---------------------------------------------------------------------------
# Largada (la marca el Motor con el tiempo de carrera)
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class ResultadoLargada:
    evento: Event
    jugadores: list[uuid.UUID]  # inscritos del torneo activo: el Motor les crea las fichas
    devueltas: int
    nueva: bool


def iniciar_evento(db: Session, event_id: str) -> ResultadoLargada:
    """Cierra inscripciones. Idempotente: repetirla devuelve el mismo resultado."""
    evento = db.scalar(select(Event).where(Event.id == event_id).with_for_update())
    if evento is None:
        raise EventoNoExiste()

    inscritos = list(
        db.scalars(select(EventEntry).where(EventEntry.event_id == event_id).order_by(EventEntry.created_at))
    )

    if evento.status != EVENT_PROGRAMADO:
        db.commit()
        activo = evento.tournament_status == TOURNAMENT_ACTIVO
        cancelado = evento.tournament_status == TOURNAMENT_CANCELADO
        return ResultadoLargada(
            evento,
            [i.user_id for i in inscritos] if activo else [],
            len(inscritos) if cancelado else 0,
            nueva=False,
        )

    evento.status = EVENT_EN_CURSO
    evento.starts_at = datetime.now(timezone.utc)

    if len(inscritos) < settings.event_min_players:
        # Pocos inscritos: no hay torneo. Se devuelve todo; la carrera sigue.
        for i in inscritos:
            _acreditar_saldo(db, i.user_id, evento.buy_in)
            db.add(
                LedgerEntry(
                    user_id=i.user_id,
                    event_id=event_id,
                    type=LEDGER_REFUND,
                    tokens_delta=evento.buy_in,
                    status=LEDGER_COMPLETED,
                    idempotency_key=clave_devolucion(event_id, i.user_id),
                )
            )
        evento.pot = 0
        evento.tournament_status = TOURNAMENT_CANCELADO
        db.commit()
        db.refresh(evento)
        return ResultadoLargada(evento, [], len(inscritos), nueva=True)

    evento.tournament_status = TOURNAMENT_ACTIVO
    db.commit()
    db.refresh(evento)
    return ResultadoLargada(evento, [i.user_id for i in inscritos], 0, nueva=True)
