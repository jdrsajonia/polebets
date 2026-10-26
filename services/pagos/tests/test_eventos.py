"""Pruebas de HU2-06: eventos multijugador e inscripciones."""
import threading
import uuid

import pytest
from sqlalchemy import delete, select, update

from app.db import SessionLocal
from app.models import (
    LEDGER_EVENT_ENTRY,
    LEDGER_EVENT_REFUND,
    Event,
    EventRegistration,
    LedgerEntry,
    User,
)
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
            db.execute(delete(EventRegistration).where(EventRegistration.event_id == eid))
            db.execute(delete(Event).where(Event.id == eid))
        db.commit()


def _crear(client, eventos, **extra):
    eid = f"evt-{uuid.uuid4().hex[:12]}"
    eventos.append(eid)
    cuerpo = {"event_id": eid, "name": "GP de prueba", "race_ref": "9161", **extra}
    r = client.post("/internal/events", json=cuerpo, headers=H)
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


# ---------------------------------------------------------------------------
# Crear evento
# ---------------------------------------------------------------------------
@requires_db
def test_crear_evento_usa_valores_por_defecto_y_es_idempotente(client, eventos):
    eid = _crear(client, eventos)
    r = client.get(f"/internal/events/{eid}", headers=H)
    e = r.json()
    assert (e["status"], e["entry_fee"], e["initial_chips"], e["commission_pct"], e["min_wager_pct"], e["min_players"], e["pot"]) == (
        "registration", 100, 1000, 10, 50, 4, 0
    )
    repetido = client.post("/internal/events", json={"event_id": eid, "name": "GP de prueba", "race_ref": "9161"}, headers=H)
    assert repetido.status_code == 200
    distinto = client.post("/internal/events", json={"event_id": eid, "name": "Otro", "race_ref": "9161"}, headers=H)
    assert distinto.status_code == 409


@requires_db
def test_rutas_internas_exigen_token_de_servicio(client):
    assert client.post("/internal/events", json={"event_id": "x", "name": "x", "race_ref": "x"}).status_code == 401
    assert client.get("/internal/events/x").status_code == 401


@requires_db
def test_la_inscripcion_minima_es_100(client):
    r = client.post("/internal/events", json={"event_id": "evt-barato", "name": "x", "race_ref": "1", "entry_fee": 50}, headers=H)
    assert r.status_code == 422


# ---------------------------------------------------------------------------
# Inscribirse
# ---------------------------------------------------------------------------
@requires_db
def test_inscribirse_cobra_suma_al_botin_y_registra_en_el_ledger(client, eventos, make_user):
    eid = _crear(client, eventos)
    uid = _jugador(make_user, saldo=500)

    r = _inscribir(client, eid, uid)

    assert r.status_code == 201, r.text
    assert (r.json()["saldo"], r.json()["pot"], r.json()["initial_chips"]) == (400, 100, 1000)
    with SessionLocal() as db:
        mov = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == clave_inscripcion(eid, uid)))
        assert (mov.type, mov.tokens_delta, mov.event_id) == (LEDGER_EVENT_ENTRY, -100, eid)
    consulta = client.get(f"/internal/events/{eid}/registrations/{uid}", headers=H)
    assert consulta.status_code == 200 and consulta.json()["status"] == "active"


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
    admin = make_user(role="admin").id

    assert _inscribir(client, eid, pobre).status_code == 422
    assert _saldo(pobre) == 99
    assert _inscribir(client, eid, admin).status_code == 403
    assert _inscribir(client, eid, pobre, clave="otra-clave").status_code == 400
    assert _inscribir(client, "evt-no-existe", pobre).status_code == 404
    assert _inscribir(client, eid, uuid.uuid4()).status_code == 404
    assert client.get(f"/internal/events/{eid}/registrations/{pobre}", headers=H).status_code == 404


# ---------------------------------------------------------------------------
# Largada
# ---------------------------------------------------------------------------
@requires_db
def test_con_menos_de_4_inscritos_se_devuelve_todo_y_queda_individual(client, eventos, make_user):
    eid = _crear(client, eventos)
    jugadores = [_jugador(make_user, saldo=300) for _ in range(3)]
    for uid in jugadores:
        assert _inscribir(client, eid, uid).status_code == 201

    r = client.post(f"/internal/events/{eid}/start", headers=H)

    assert r.status_code == 200
    assert (r.json()["status"], r.json()["pot"], r.json()["players"], r.json()["refunded"]) == ("individual_only", 0, [], 3)
    assert [_saldo(u) for u in jugadores] == [300, 300, 300]
    with SessionLocal() as db:
        devoluciones = db.scalars(select(LedgerEntry).where(LedgerEntry.event_id == eid, LedgerEntry.type == LEDGER_EVENT_REFUND)).all()
        assert len(devoluciones) == 3 and all(d.tokens_delta == 100 for d in devoluciones)

    # Repetir la largada no devuelve otra vez.
    otra = client.post(f"/internal/events/{eid}/start", headers=H)
    assert otra.json()["nueva"] is False
    assert [_saldo(u) for u in jugadores] == [300, 300, 300]


@requires_db
def test_con_4_inscritos_hay_multijugador_y_se_cierran_las_inscripciones(client, eventos, make_user):
    eid = _crear(client, eventos)
    jugadores = [_jugador(make_user, saldo=300) for _ in range(4)]
    for uid in jugadores:
        _inscribir(client, eid, uid)

    r = client.post(f"/internal/events/{eid}/start", headers=H)

    assert (r.json()["status"], r.json()["pot"], r.json()["initial_chips"]) == ("multiplayer", 400, 1000)
    assert sorted(r.json()["players"]) == sorted(str(u) for u in jugadores)
    tarde = _jugador(make_user, saldo=300)
    assert _inscribir(client, eid, tarde).status_code == 409
    assert _saldo(tarde) == 300


@requires_db
def test_la_largada_y_una_inscripcion_no_se_cruzan(client, eventos, make_user):
    """Con el evento bloqueado, o la inscripcion entra antes y cuenta, o llega tarde y se rechaza."""
    eid = _crear(client, eventos, min_players=2)
    uid = _jugador(make_user, saldo=300)
    estado = {}

    def largar():
        estado["largada"] = client.post(f"/internal/events/{eid}/start", headers=H).json()

    def inscribirse():
        with SessionLocal() as db:
            try:
                inscribir(db, eid, uid)
                estado["inscrito"] = True
            except InscripcionCerrada:
                estado["inscrito"] = False

    hilos = [threading.Thread(target=largar), threading.Thread(target=inscribirse)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    # Con un solo inscrito y minimo 2, si entro se le devolvio; si no entro, nunca se cobro.
    assert _saldo(uid) == 300
    assert estado["largada"]["status"] == "individual_only"
