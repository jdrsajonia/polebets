"""Pruebas de HU2-06 (parte Pagos): eventos e inscripcion al torneo."""
import threading
import uuid

import pytest
from sqlalchemy import delete, select, update

from app.core.config import settings
from app.db import SessionLocal
from app.models import LEDGER_BUY_IN, LEDGER_REFUND, Event, EventEntry, LedgerEntry, User
from app.services.eventos import InscripcionCerrada, clave_inscripcion, inscribir
from tests.conftest import requires_db

TOKEN = "token-interno-de-pruebas-para-hu07"
H = {"X-Internal-Service-Token": TOKEN}


@pytest.fixture
def eventos():
    """Ids de eventos creados en la prueba; se borran al terminar."""
    creados: list[str] = []
    yield creados
    with SessionLocal() as db:
        for eid in creados:
            db.execute(delete(LedgerEntry).where(LedgerEntry.event_id == eid))
            db.execute(delete(EventEntry).where(EventEntry.event_id == eid))
            db.execute(delete(Event).where(Event.id == eid))
        db.commit()


def _crear(client, eventos, **extra):
    eid = f"evt-{uuid.uuid4().hex[:12]}"
    eventos.append(eid)
    r = client.post("/internal/events", json={"event_id": eid, "race_key": "2024_monza", **extra}, headers=H)
    assert r.status_code == 201, r.text
    return eid


def _jugador(make_user, saldo=500):
    u = make_user()
    with SessionLocal() as db:
        db.execute(update(User).where(User.id == u.id).values(balance=saldo))
        db.commit()
    return u.id


def _saldo(uid):
    with SessionLocal() as db:
        return db.scalar(select(User.balance).where(User.id == uid))


def _inscribir(client, eid, uid, clave=None):
    return client.post(
        f"/internal/events/{eid}/registrations",
        json={"user_id": str(uid)},
        headers={**H, "Idempotency-Key": clave or clave_inscripcion(eid, uid)},
    )


def _largar(client, eid):
    return client.post(f"/internal/events/{eid}/start", headers=H)


# ---------------------------------------------------------------------------
# Crear evento
# ---------------------------------------------------------------------------
@requires_db
def test_crear_evento_usa_la_configuracion_y_es_idempotente(client, eventos):
    eid = _crear(client, eventos)
    e = client.get(f"/internal/events/{eid}", headers=H).json()
    assert (e["status"], e["tournament_status"], e["buy_in"], e["initial_chips"], e["commission_pct"], e["pot"]) == (
        "programado", "abierto", 100, 1000, 10, 0
    )
    repetido = client.post("/internal/events", json={"event_id": eid, "race_key": "2024_monza"}, headers=H)
    assert repetido.status_code == 200
    distinto = client.post("/internal/events", json={"event_id": eid, "race_key": "2024_spa"}, headers=H)
    assert distinto.status_code == 409


@requires_db
def test_rutas_internas_exigen_token_de_servicio(client):
    assert client.post("/internal/events", json={"event_id": "x", "race_key": "x"}).status_code == 401
    assert client.get("/internal/events/x").status_code == 401


@requires_db
def test_el_buy_in_debe_ser_positivo_pero_no_tiene_minimo_fijo(client, eventos):
    assert client.post("/internal/events", json={"event_id": "evt-gratis", "race_key": "1", "buy_in": 0}, headers=H).status_code == 422
    eid = _crear(client, eventos, buy_in=50)
    assert client.get(f"/internal/events/{eid}", headers=H).json()["buy_in"] == 50


# ---------------------------------------------------------------------------
# Inscribirse
# ---------------------------------------------------------------------------
@requires_db
def test_inscribirse_cobra_suma_al_botin_y_registra_buy_in(client, eventos, make_user):
    eid = _crear(client, eventos)
    uid = _jugador(make_user, saldo=500)

    r = _inscribir(client, eid, uid)

    assert r.status_code == 201, r.text
    assert (r.json()["saldo"], r.json()["pot"], r.json()["initial_chips"], r.json()["refunded"]) == (400, 100, 1000, False)
    with SessionLocal() as db:
        mov = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == clave_inscripcion(eid, uid)))
        assert (mov.type, mov.tokens_delta, mov.event_id) == (LEDGER_BUY_IN, -100, eid)
    consulta = client.get(f"/internal/events/{eid}/registrations/{uid}", headers=H)
    assert consulta.status_code == 200 and consulta.json()["tournament_status"] == "abierto"


@requires_db
def test_inscribirse_dos_veces_no_cobra_doble(client, eventos, make_user):
    eid = _crear(client, eventos)
    uid = _jugador(make_user, saldo=500)

    assert _inscribir(client, eid, uid).status_code == 201
    otra = _inscribir(client, eid, uid)

    assert otra.status_code == 200 and otra.json()["nueva"] is False
    assert _saldo(uid) == 400
    assert client.get(f"/internal/events/{eid}", headers=H).json()["pot"] == 100


@requires_db
def test_inscripciones_simultaneas_del_mismo_jugador_cobran_una_vez(client, eventos, make_user):
    eid = _crear(client, eventos)
    uid = _jugador(make_user, saldo=500)
    resultados = []

    def intentar():
        with SessionLocal() as db:
            resultados.append(inscribir(db, eid, uid).nueva)

    hilos = [threading.Thread(target=intentar) for _ in range(5)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    assert sorted(resultados) == [False, False, False, False, True]
    assert _saldo(uid) == 400


@requires_db
def test_errores_de_inscripcion(client, eventos, make_user):
    eid = _crear(client, eventos)
    pobre = _jugador(make_user, saldo=99)

    assert _inscribir(client, eid, pobre).status_code == 422
    assert _saldo(pobre) == 99
    assert _inscribir(client, eid, pobre, clave="otra-clave").status_code == 400
    assert _inscribir(client, "evt-no-existe", pobre).status_code == 404
    # Sin billetera (usuario desconocido para Pagos): no hay saldo -> 422. El rol lo valida el Orquestador.
    assert _inscribir(client, eid, uuid.uuid4()).status_code == 422
    assert client.get(f"/internal/events/{eid}/registrations/{pobre}", headers=H).status_code == 404


# ---------------------------------------------------------------------------
# Largada
# ---------------------------------------------------------------------------
@requires_db
def test_con_menos_de_4_inscritos_se_cancela_el_torneo_y_se_devuelve_todo(client, eventos, make_user):
    eid = _crear(client, eventos)
    jugadores = [_jugador(make_user, saldo=300) for _ in range(3)]
    for uid in jugadores:
        assert _inscribir(client, eid, uid).status_code == 201

    r = _largar(client, eid).json()

    assert (r["status"], r["tournament_status"], r["pot"], r["players"], r["refunded"]) == ("en_curso", "cancelado", 0, [], 3)
    assert [_saldo(u) for u in jugadores] == [300, 300, 300]
    with SessionLocal() as db:
        devoluciones = db.scalars(select(LedgerEntry).where(LedgerEntry.event_id == eid, LedgerEntry.type == LEDGER_REFUND)).all()
        assert len(devoluciones) == 3 and all(d.tokens_delta == 100 for d in devoluciones)
    # Las inscripciones quedan como historial y se marcan como devueltas.
    assert client.get(f"/internal/events/{eid}", headers=H).json()["registered"] == 3
    assert client.get(f"/internal/events/{eid}/registrations/{jugadores[0]}", headers=H).json()["refunded"] is True

    # Repetir la largada no devuelve otra vez.
    otra = _largar(client, eid).json()
    assert (otra["nueva"], otra["refunded"]) == (False, 3)
    assert [_saldo(u) for u in jugadores] == [300, 300, 300]


@requires_db
def test_con_4_inscritos_el_torneo_queda_activo_y_se_cierran_las_inscripciones(client, eventos, make_user):
    eid = _crear(client, eventos)
    jugadores = [_jugador(make_user, saldo=300) for _ in range(4)]
    for uid in jugadores:
        _inscribir(client, eid, uid)

    r = _largar(client, eid).json()

    assert (r["status"], r["tournament_status"], r["pot"], r["initial_chips"]) == ("en_curso", "activo", 400, 1000)
    assert sorted(r["players"]) == sorted(str(u) for u in jugadores)
    tarde = _jugador(make_user, saldo=300)
    assert _inscribir(client, eid, tarde).status_code == 409
    assert _saldo(tarde) == 300


@requires_db
def test_la_largada_y_una_inscripcion_no_se_cruzan(client, eventos, make_user, monkeypatch):
    """Con el evento bloqueado, o la inscripcion entra antes y cuenta, o llega tarde y se rechaza."""
    monkeypatch.setattr(settings, "event_min_players", 2)
    eid = _crear(client, eventos)
    uid = _jugador(make_user, saldo=300)
    estado = {}

    def largar():
        estado["largada"] = _largar(client, eid).json()

    def inscribirse():
        with SessionLocal() as db:
            try:
                inscribir(db, eid, uid)
            except InscripcionCerrada:
                pass

    hilos = [threading.Thread(target=largar), threading.Thread(target=inscribirse)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    # Un solo jugador con minimo 2: si entro se le devolvio; si no entro, nunca se cobro.
    assert _saldo(uid) == 300
    assert estado["largada"]["tournament_status"] == "cancelado"
