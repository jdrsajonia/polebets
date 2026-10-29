"""Endpoints internos de la billetera (T2-03, contrato del Orquestador §5)."""
import threading
import uuid

from sqlalchemy import update

from app.db import SessionLocal
from app.models import Wallet
from app.services.billetera import crear_billetera, saldo_de, tasa
from tests.conftest import headers, requires_db


def _crear(client, user_id, clave=None):
    clave = clave if clave is not None else f"wallet:{user_id}"
    return client.post("/internal/wallets", json={"user_id": str(user_id)}, headers=headers(clave))


@requires_db
def test_el_registro_crea_la_billetera_con_saldo_0(client, fabrica):
    user_id = fabrica.usuario_sin_billetera()

    r = _crear(client, user_id)
    assert r.status_code == 201
    assert r.json() == {"user_id": str(user_id), "balance": 0, "cop_por_token": tasa()}


@requires_db
def test_reintentar_la_creacion_responde_200_y_no_toca_el_saldo(client, fabrica):
    user_id = fabrica.usuario_sin_billetera()
    assert _crear(client, user_id).status_code == 201
    with SessionLocal() as db:
        db.execute(update(Wallet).where(Wallet.user_id == user_id).values(balance=40))
        db.commit()

    r = _crear(client, user_id)
    assert r.status_code == 200
    assert r.json()["balance"] == 40


@requires_db
def test_la_clave_debe_ser_wallet_user_id(client, fabrica):
    user_id = fabrica.usuario_sin_billetera()
    for clave in ("", "otra", f"wallet:{uuid.uuid4()}"):
        assert _crear(client, user_id, clave).status_code == 400
    with SessionLocal() as db:
        assert saldo_de(db, user_id) is None


@requires_db
def test_dos_creaciones_simultaneas_dejan_una_billetera(fabrica):
    user_id = fabrica.usuario_sin_billetera()
    barrera = threading.Barrier(2)
    nuevas, errores = [], []

    def crear():
        try:
            with SessionLocal() as db:
                barrera.wait()
                nuevas.append(crear_billetera(db, user_id)[1])
        except Exception as error:  # noqa: BLE001  (se revisa abajo)
            errores.append(error)

    hilos = [threading.Thread(target=crear) for _ in range(2)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    assert errores == []
    assert sorted(nuevas) == [False, True]


@requires_db
def test_consultar_saldo_y_tasa(client, fabrica):
    user_id = fabrica.billetera(balance=1250)

    r = client.get(f"/internal/wallets/{user_id}", headers=headers())
    assert r.status_code == 200
    assert r.json() == {"user_id": str(user_id), "balance": 1250, "cop_por_token": tasa()}
    assert r.headers["cache-control"] == "no-store"


@requires_db
def test_usuario_sin_billetera_responde_404(client):
    assert client.get(f"/internal/wallets/{uuid.uuid4()}", headers=headers()).status_code == 404


@requires_db
def test_sin_token_de_servicio_responde_401(client, fabrica):
    user_id = fabrica.billetera()
    assert client.get(f"/internal/wallets/{user_id}").status_code == 401
    assert client.get(f"/internal/wallets/{user_id}", headers=headers(token="incorrecto")).status_code == 401
    r = client.post("/internal/wallets", json={"user_id": str(uuid.uuid4())}, headers={"Idempotency-Key": "x"})
    assert r.status_code == 401
