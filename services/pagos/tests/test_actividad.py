"""HU2-03 (parte Pagos): actividad del jugador para la pagina SSR."""
import uuid
from datetime import datetime, timedelta, timezone
from decimal import Decimal

from app.db import SessionLocal
from app.models import BET_INDIVIDUAL, BET_PENDING, BET_WON, Bet, EventEntry, LedgerEntry
from tests.conftest import headers, requires_db


def _apuesta(user_id, mercado, status, minutos):
    return Bet(id=uuid.uuid4(), user_id=user_id, market_id=mercado.id, event_id=mercado.event_id,
               mode=BET_INDIVIDUAL, option="VER", amount=50, multiplier=Decimal("2.5"), status=status,
               payout=125 if status == BET_WON else None,
               created_at=datetime.now(timezone.utc) - timedelta(minutes=minutos))


@requires_db
def test_inscripciones_con_su_evento(client, fabrica):
    jugador = fabrica.billetera(500)
    evento = fabrica.evento()
    with SessionLocal() as db:
        db.add(EventEntry(event_id=evento.id, user_id=jugador, chips_final=1340, position_final=2, prize=90))
        db.commit()

    r = client.get(f"/internal/users/{jugador}/event-entries", headers=headers())

    assert r.status_code == 200
    assert r.headers["cache-control"] == "no-store"
    [entrada] = r.json()
    assert entrada["event_id"] == str(evento.id) and entrada["race_key"] == evento.race_key
    assert (entrada["status"], entrada["tournament_status"]) == ("programado", "abierto")
    assert (entrada["chips_final"], entrada["position_final"], entrada["prize"]) == (1340, 2, 90)


@requires_db
def test_apuestas_filtradas_por_estado_y_mas_recientes_primero(client, fabrica):
    jugador = fabrica.billetera(500)
    m1, m2, m3 = fabrica.mercado(), fabrica.mercado(), fabrica.mercado()
    with SessionLocal() as db:
        db.add_all([_apuesta(jugador, m1, BET_PENDING, 10), _apuesta(jugador, m2, BET_PENDING, 1),
                    _apuesta(jugador, m3, BET_WON, 5)])
        db.commit()

    pendientes = client.get(f"/internal/users/{jugador}/bets?status=pending", headers=headers()).json()
    todas = client.get(f"/internal/users/{jugador}/bets", headers=headers()).json()

    assert [a["market_id"] for a in pendientes] == [str(m2.id), str(m1.id)]
    assert pendientes[0]["multiplier"] == 2.5 and pendientes[0]["mode"] == "individual"
    assert len(todas) == 3
    assert client.get(f"/internal/users/{jugador}/bets?status=otro", headers=headers()).status_code == 422


@requires_db
def test_ultimos_movimientos_con_limite(client, fabrica):
    jugador = fabrica.billetera(0)
    ahora = datetime.now(timezone.utc)
    with SessionLocal() as db:
        db.add_all([
            LedgerEntry(user_id=jugador, type="purchase", tokens_delta=10 * (i + 1),
                        idempotency_key=f"prueba-actividad:{jugador}:{i}", created_at=ahora - timedelta(minutes=12 - i))
            for i in range(12)
        ])
        db.commit()

    r = client.get(f"/internal/users/{jugador}/ledger?limit=10", headers=headers())

    assert r.status_code == 200
    movimientos = r.json()
    assert len(movimientos) == 10
    assert movimientos[0] == {"type": "purchase", "tokens_delta": 120, "event_id": None,
                              "created_at": movimientos[0]["created_at"]}
    assert [m["tokens_delta"] for m in movimientos] == sorted((m["tokens_delta"] for m in movimientos), reverse=True)
    assert client.get(f"/internal/users/{jugador}/ledger?limit=0", headers=headers()).status_code == 422


@requires_db
def test_sin_actividad_son_listas_vacias_y_exige_token(client, fabrica):
    nuevo = fabrica.usuario_sin_billetera()
    for ruta in ("event-entries", "bets?status=pending", "ledger"):
        assert client.get(f"/internal/users/{nuevo}/{ruta}", headers=headers()).json() == []
        assert client.get(f"/internal/users/{nuevo}/{ruta}").status_code == 401
