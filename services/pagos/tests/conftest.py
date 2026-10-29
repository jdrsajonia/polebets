"""Configuracion de pruebas.

Las pruebas que tocan la BD necesitan un PostgreSQL. El de docker compose no
publica su puerto, asi que lo mas facil es uno aparte:

    docker run -d --name pg-pruebas -e POSTGRES_USER=arqui -e POSTGRES_PASSWORD=arqui_pass \\
        -e POSTGRES_DB=pagos_db -p 55432:5432 postgres:17-alpine
    cd services/pagos
    DATABASE_URL=postgresql+psycopg://arqui:arqui_pass@localhost:55432/pagos_db pytest

Si PostgreSQL no esta disponible, esas pruebas se saltan (skip) y las demas corren.
"""
import os
import uuid
from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest

os.environ.setdefault("DATABASE_URL", "postgresql+psycopg://arqui:arqui_pass@localhost:5432/pagos_db")
os.environ["INTERNAL_API_TOKEN"] = "token-interno-de-pruebas-de-pagos-32c"
os.environ.setdefault("DB_CONNECT_RETRIES", "1")
os.environ.setdefault("DB_CONNECT_RETRY_SECONDS", "0")

from app.db import ping_db  # noqa: E402

TOKEN = os.environ["INTERNAL_API_TOKEN"]
DB_AVAILABLE = ping_db()
requires_db = pytest.mark.skipif(not DB_AVAILABLE, reason="PostgreSQL no disponible")

if DB_AVAILABLE:
    from app.db import init_db

    init_db()


def headers(clave=None, token=TOKEN):
    h = {"X-Internal-Service-Token": token} if token else {}
    if clave:
        h["Idempotency-Key"] = clave
    return h


@pytest.fixture
def client():
    from fastapi.testclient import TestClient

    from app.main import app

    with TestClient(app) as c:  # "with" ejecuta el lifespan (crea tablas)
        yield c


class Fabrica:
    """Crea billeteras, eventos y mercados de prueba y recuerda que borrar."""

    def __init__(self):
        self.usuarios: list[uuid.UUID] = []
        self.eventos: list[uuid.UUID] = []

    def billetera(self, balance=0) -> uuid.UUID:
        from app.db import SessionLocal
        from app.models import Wallet

        user_id = uuid.uuid4()
        with SessionLocal() as db:
            db.add(Wallet(user_id=user_id, balance=balance))
            db.commit()
        self.usuarios.append(user_id)
        return user_id

    def usuario_sin_billetera(self) -> uuid.UUID:
        user_id = uuid.uuid4()
        self.usuarios.append(user_id)
        return user_id

    def evento(self):
        from app.db import SessionLocal
        from app.models import Event

        with SessionLocal() as db:
            evento = Event(
                race_key=f"prueba-{uuid.uuid4().hex[:8]}",
                starts_at=datetime.now(timezone.utc) + timedelta(minutes=10),
                buy_in=100,
                initial_chips=1000,
                commission_pct=Decimal("10"),
            )
            db.add(evento)
            db.commit()
            self.eventos.append(evento.id)
            return evento

    def mercado(self, event_id=None):
        from app.db import SessionLocal
        from app.models import MARKET_ESTATICA, Market

        event_id = event_id or self.evento().id
        with SessionLocal() as db:
            mercado = Market(
                event_id=event_id,
                kind=MARKET_ESTATICA,
                template="ganador",
                question="¿Quién gana la carrera?",
                options=[{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3}],
            )
            db.add(mercado)
            db.commit()
            return mercado

    def limpiar(self):
        from sqlalchemy import delete, or_

        from app.db import SessionLocal
        from app.models import Bet, Event, EventEntry, LedgerEntry, Market, Wallet

        with SessionLocal() as db:
            db.execute(
                delete(LedgerEntry).where(
                    or_(LedgerEntry.user_id.in_(self.usuarios), LedgerEntry.event_id.in_(self.eventos))
                )
            )
            db.execute(delete(Bet).where(or_(Bet.user_id.in_(self.usuarios), Bet.event_id.in_(self.eventos))))
            db.execute(delete(Market).where(Market.event_id.in_(self.eventos)))
            db.execute(delete(EventEntry).where(EventEntry.event_id.in_(self.eventos)))
            db.execute(delete(Event).where(Event.id.in_(self.eventos)))
            db.execute(delete(Wallet).where(Wallet.user_id.in_(self.usuarios)))
            db.commit()


@pytest.fixture
def fabrica():
    f = Fabrica()
    yield f
    f.limpiar()
