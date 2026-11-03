"""HU2-22 (RF-27): ganancia y volumen por franja horaria y por vuelta."""
import json
import uuid
from datetime import datetime, timedelta, timezone
from decimal import Decimal

from app.db import SessionLocal
from app.models import BET_INDIVIDUAL, BET_LOST, BET_PENDING, BET_TORNEO, BET_WON, MARKET_DINAMICA, Bet, Market
from app.services.consulta_handler import procesar_consulta
from app.services.horarios import calcular_horarios
from app.services.resumen_financiero import FiltrosResumen
from tests.conftest import requires_db

UTC = timezone.utc


def _horarios(**filtros):
    with SessionLocal() as db:
        return calcular_horarios(db, FiltrosResumen(**filtros))


def _apuesta(fabrica, market_id, event_id, amount, status, payout=None, lap=None, creada=None, mode=BET_INDIVIDUAL):
    with SessionLocal() as db:
        b = Bet(
            id=uuid.uuid4(),
            user_id=fabrica.billetera(),
            market_id=market_id,
            event_id=event_id,
            mode=mode,
            option="VER",
            amount=amount,
            multiplier=Decimal("2"),
            lap=lap,
            status=status,
            payout=payout,
        )
        if creada is not None:
            b.created_at = creada
        db.add(b)
        db.commit()


def _sembrar(fabrica):
    """Un evento con apuestas en dos franjas y dos vueltas (y una sin vuelta).

    03:00 UTC = 22:00 en Colombia (UTC-5); 15:30 UTC = 10:30 en Colombia.
    """
    estatica = fabrica.mercado()
    event_id = estatica.event_id
    with SessionLocal() as db:
        dinamica = Market(event_id=event_id, kind=MARKET_DINAMICA, template="lider", question="¿?",
                          options=[{"opcion": "VER", "multiplicador": 2}])
        db.add(dinamica)
        db.commit()
        dinamica_id = dinamica.id
    noche = datetime(2026, 10, 20, 3, 0, tzinfo=UTC)
    tarde = datetime(2026, 10, 20, 15, 30, tzinfo=UTC)
    _apuesta(fabrica, estatica.id, event_id, 100, BET_WON, 200, lap=None, creada=noche)  # ganancia -100
    _apuesta(fabrica, dinamica_id, event_id, 50, BET_LOST, 0, lap=12, creada=tarde)  # ganancia 50
    _apuesta(fabrica, dinamica_id, event_id, 30, BET_PENDING, lap=12, creada=tarde)  # ganancia 30 (sin resolver)
    _apuesta(fabrica, dinamica_id, event_id, 20, BET_LOST, 0, lap=3, creada=tarde)  # ganancia 20
    _apuesta(fabrica, dinamica_id, event_id, 500, BET_PENDING, lap=3, creada=tarde, mode=BET_TORNEO)  # no cuenta
    return event_id


def _tabla(filas):
    return [(f.clave, f.volumen, f.ganancia) for f in filas]


@requires_db
def test_por_hora_en_hora_de_colombia(fabrica):
    event_id = _sembrar(fabrica)

    assert _tabla(_horarios(event_id=event_id).por_hora) == [(10, 100, 100), (22, 100, -100)]


@requires_db
def test_por_vuelta_con_grupo_sin_vuelta(fabrica):
    event_id = _sembrar(fabrica)

    assert _tabla(_horarios(event_id=event_id).por_vuelta) == [(None, 100, -100), (3, 20, 20), (12, 80, 80)]


@requires_db
def test_filtros(fabrica):
    event_id = _sembrar(fabrica)

    assert _tabla(_horarios(event_id=event_id, kind="estatica").por_hora) == [(22, 100, -100)]
    assert _tabla(_horarios(event_id=event_id, kind="dinamica").por_vuelta) == [(3, 20, 20), (12, 80, 80)]
    tarde = datetime(2026, 10, 20, 15, 0, tzinfo=UTC)
    assert _tabla(_horarios(event_id=event_id, desde=tarde, hasta=tarde + timedelta(hours=1)).por_hora) == [(10, 100, 100)]
    assert _tabla(_horarios(event_id=event_id, modo="individual").por_hora) == [(10, 100, 100), (22, 100, -100)]
    # Las apuestas de torneo no estan en Pagos: con modo torneo no hay datos.
    torneo = _horarios(event_id=event_id, modo="torneo")
    assert (torneo.por_hora, torneo.por_vuelta) == ([], [])


@requires_db
def test_consulta_por_el_broker(fabrica):
    event_id = _sembrar(fabrica)
    mensaje = {"tipo": "horarios", "filtros": {"event_id": str(event_id)}}

    with SessionLocal() as db:
        r = json.loads(procesar_consulta(db, json.dumps(mensaje).encode()))
        assert (r["tipo"], r["ok"]) == ("horarios", True)
        assert r["datos"]["por_hora"][0] == {"hora": 10, "volumen": 100, "ganancia": 100}
        assert r["datos"]["por_vuelta"][0] == {"vuelta": None, "volumen": 100, "ganancia": -100}

        malo = json.dumps({"tipo": "horarios", "filtros": {"top_n": 3}}).encode()
        assert json.loads(procesar_consulta(db, malo))["error"] == "filtros_invalidos"
