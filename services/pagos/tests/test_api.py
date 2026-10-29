"""Arranque del servicio y tablas de T2-03."""
from sqlalchemy import inspect

from app.db import Base, engine
from tests.conftest import requires_db


@requires_db
def test_health_y_tablas_del_prototipo_2(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"service": "pagos", "status": "ok", "postgres": True}

    tablas = set(inspect(engine).get_table_names())
    assert {"wallets", "events", "event_entries", "markets", "bets", "ledger_entries"} <= tablas
    # users vive en PostgreSQL Usuarios (T2-03): Pagos no la crea.
    assert "users" not in Base.metadata.tables


@requires_db
def test_pagos_no_expone_rutas_de_sesion(client):
    # El login y la billetera publica pasaron a Usuarios y al Orquestador.
    assert client.post("/auth/login", json={}).status_code == 404
    assert client.get("/billetera").status_code == 404
