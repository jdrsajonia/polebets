"""Pruebas de HU2-09 (parte Pagos): lectura de un mercado para el Motor."""
import uuid

from tests.conftest import headers, requires_db


@requires_db
def test_el_motor_lee_el_mercado_con_sus_opciones_y_multiplicadores(client, fabrica):
    mercado = fabrica.mercado()

    r = client.get(f"/internal/markets/{mercado.id}", headers=headers())

    assert r.status_code == 200, r.text
    m = r.json()
    assert (m["market_id"], m["event_id"], m["kind"], m["status"]) == (str(mercado.id), str(mercado.event_id), "estatica", "programado")
    assert m["options"] == [{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3.0}]
    assert r.headers["cache-control"] == "no-store"


@requires_db
def test_mercado_inexistente_y_token(client):
    assert client.get(f"/internal/markets/{uuid.uuid4()}", headers=headers()).status_code == 404
    assert client.get(f"/internal/markets/{uuid.uuid4()}").status_code == 401


# --- HU2-10: liquidacion individual (lo que el Motor necesita de Pagos) ---


def _cerrar(mercado_id, ganadora="VER"):
    from app.db import SessionLocal
    from app.models import MARKET_CERRADO, Market

    with SessionLocal() as db:
        m = db.get(Market, mercado_id)
        m.status = MARKET_CERRADO
        m.winning_option = ganadora
        db.commit()


def _apostar(client, fabrica, mercado, opcion="VER", monto=100):
    user_id = fabrica.billetera(balance=1000)
    bet_id = uuid.uuid4()
    clave = f"bet-debit:{bet_id}"
    r = client.post(
        "/internal/debit",
        headers=headers(clave),
        json={
            "bet_id": str(bet_id),
            "user_id": str(user_id),
            "market_id": str(mercado.id),
            "option": opcion,
            "amount": monto,
            "multiplier": "2.5" if opcion == "VER" else "3",
            "lap": None,
            "idempotency_key": clave,
        },
    )
    assert r.status_code == 201, r.text
    return bet_id, user_id


@requires_db
def test_lista_solo_los_mercados_cerrados_con_ganador(client, fabrica):
    evento = fabrica.evento()
    listo = fabrica.mercado(evento.id)
    sin_ganador = fabrica.mercado(evento.id)
    abierto = fabrica.mercado(evento.id)
    _cerrar(listo.id)
    _cerrar(sin_ganador.id, ganadora=None)

    r = client.get("/internal/markets?status=cerrado&resuelto=true&limit=200", headers=headers())

    assert r.status_code == 200, r.text
    ids = {m["market_id"] for m in r.json()}
    assert str(listo.id) in ids
    assert str(sin_ganador.id) not in ids and str(abierto.id) not in ids
    assert next(m for m in r.json() if m["market_id"] == str(listo.id))["winning_option"] == "VER"


@requires_db
def test_apuestas_pendientes_individuales_del_mercado(client, fabrica):
    mercado = fabrica.mercado()
    bet_ver, user_ver = _apostar(client, fabrica, mercado, "VER", 100)
    bet_nor, _ = _apostar(client, fabrica, mercado, "NOR", 50)

    r = client.get(f"/internal/markets/{mercado.id}/bets?mode=individual&status=pending", headers=headers())

    assert r.status_code == 200, r.text
    por_id = {b["bet_id"]: b for b in r.json()}
    assert set(por_id) == {str(bet_ver), str(bet_nor)}
    b = por_id[str(bet_ver)]
    assert (b["user_id"], b["option"], b["amount"], b["multiplier"], b["status"], b["payout"]) == (
        str(user_ver), "VER", 100, 2.5, "pending", None,
    )
    assert client.get(f"/internal/markets/{uuid.uuid4()}/bets", headers=headers()).status_code == 404


@requires_db
def test_marcar_liquidado_solo_cuando_no_quedan_pendientes(client, fabrica):
    mercado = fabrica.mercado()
    bet_id, _ = _apostar(client, fabrica, mercado, "VER", 100)
    url = f"/internal/markets/{mercado.id}/liquidated"

    # Abierto/sin ganador: no se puede.
    assert client.post(url, headers=headers()).status_code == 409
    _cerrar(mercado.id)
    # Cerrado con ganador pero con una apuesta individual sin pagar: tampoco.
    assert client.post(url, headers=headers()).status_code == 409

    clave = f"bet-settlement:{bet_id}"
    r = client.post("/internal/settlements", headers=headers(clave), json={"bet_id": str(bet_id), "result": "won", "idempotency_key": clave})
    assert r.status_code == 201, r.text

    r = client.post(url, headers=headers())
    assert r.status_code == 200 and r.json() == {"market_id": str(mercado.id), "status": "liquidado", "nueva": True}
    # Repetir no cambia nada.
    assert client.post(url, headers=headers()).json()["nueva"] is False
    assert client.get(f"/internal/markets/{mercado.id}", headers=headers()).json()["status"] == "liquidado"
    assert client.post(f"/internal/markets/{uuid.uuid4()}/liquidated", headers=headers()).status_code == 404


# --- Escritura de mercados (HU2-07 / HU2-08) ---


def _cuerpo(event_id, **cambios):
    cuerpo = {
        "market_id": str(uuid.uuid4()),
        "event_id": str(event_id),
        "kind": "estatica",
        "template": "ganador",
        "question": "¿Quién gana la carrera?",
        "options": [{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3}],
    }
    cuerpo.update(cambios)
    return cuerpo


def _crear(client, fabrica, **cambios):
    cuerpo = _cuerpo(fabrica.evento().id, **cambios)
    r = client.post("/internal/markets", headers=headers(), json=cuerpo)
    assert r.status_code == 201, r.text
    return cuerpo["market_id"]


@requires_db
def test_crear_mercado_devuelve_201(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id, race_close_s=120)

    r = client.post("/internal/markets", headers=headers(), json=cuerpo)

    assert r.status_code == 201, r.text
    m = r.json()
    assert (m["market_id"], m["status"], m["kind"], m["race_close_s"], m["winning_option"]) == (
        cuerpo["market_id"], "programado", "estatica", 120, None
    )
    assert m["options"] == [{"opcion": "VER", "multiplicador": 2.5}, {"opcion": "NOR", "multiplicador": 3.0}]
    assert r.headers["cache-control"] == "no-store"


@requires_db
def test_reintento_igual_devuelve_200_y_no_duplica(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id)
    assert client.post("/internal/markets", headers=headers(), json=cuerpo).status_code == 201

    r = client.post("/internal/markets", headers=headers(), json=cuerpo)

    assert r.status_code == 200, r.text
    assert r.json()["market_id"] == cuerpo["market_id"]


@requires_db
def test_reintento_tras_abrir_sigue_siendo_200(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id)
    client.post("/internal/markets", headers=headers(), json=cuerpo)
    client.post(f"/internal/markets/{cuerpo['market_id']}/open", headers=headers())

    r = client.post("/internal/markets", headers=headers(), json=cuerpo)

    assert r.status_code == 200, r.text
    assert r.json()["status"] == "abierto"  # el status no cuenta en la comparacion


@requires_db
def test_mismo_id_con_otros_datos_devuelve_409(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id)
    client.post("/internal/markets", headers=headers(), json=cuerpo)

    r = client.post("/internal/markets", headers=headers(), json={**cuerpo, "question": "Otra pregunta"})

    assert r.status_code == 409, r.text


@requires_db
def test_crear_con_evento_inexistente_devuelve_404(client):
    r = client.post("/internal/markets", headers=headers(), json=_cuerpo(uuid.uuid4()))

    assert r.status_code == 404
    assert r.json()["detail"] == "Evento inexistente"


@requires_db
def test_crear_valida_las_opciones(client, fabrica):
    evento = fabrica.evento().id
    repetidas = [{"opcion": "VER", "multiplicador": 2}, {"opcion": "VER", "multiplicador": 3}]
    una_sola = [{"opcion": "VER", "multiplicador": 2}]
    cero = [{"opcion": "VER", "multiplicador": 0}, {"opcion": "NOR", "multiplicador": 3}]

    for opciones in (repetidas, una_sola, cero):
        r = client.post("/internal/markets", headers=headers(), json=_cuerpo(evento, options=opciones))
        assert r.status_code == 422, r.text


@requires_db
def test_escritura_de_mercados_exige_token(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id)
    mid = uuid.uuid4()

    assert client.post("/internal/markets", json=cuerpo).status_code == 401
    assert client.post(f"/internal/markets/{mid}/open").status_code == 401
    assert client.post(f"/internal/markets/{mid}/close").status_code == 401
    assert client.post(f"/internal/markets/{mid}/resolve", json={"winning_option": "VER"}).status_code == 401


@requires_db
def test_abrir_y_cerrar(client, fabrica):
    mid = _crear(client, fabrica)

    abierto = client.post(f"/internal/markets/{mid}/open", headers=headers())
    cerrado = client.post(f"/internal/markets/{mid}/close", headers=headers())

    assert (abierto.status_code, abierto.json()["status"]) == (200, "abierto")
    assert (cerrado.status_code, cerrado.json()["status"]) == (200, "cerrado")


@requires_db
def test_abrir_un_mercado_abierto_no_cambia_nada(client, fabrica):
    mid = _crear(client, fabrica, status="abierto")

    r = client.post(f"/internal/markets/{mid}/open", headers=headers())

    assert (r.status_code, r.json()["status"]) == (200, "abierto")


@requires_db
def test_abrir_un_mercado_cerrado_devuelve_409(client, fabrica):
    mid = _crear(client, fabrica)
    client.post(f"/internal/markets/{mid}/close", headers=headers())

    assert client.post(f"/internal/markets/{mid}/open", headers=headers()).status_code == 409


@requires_db
def test_cerrar_dos_veces_devuelve_200(client, fabrica):
    mid = _crear(client, fabrica, status="abierto")
    client.post(f"/internal/markets/{mid}/close", headers=headers())

    r = client.post(f"/internal/markets/{mid}/close", headers=headers())

    assert (r.status_code, r.json()["status"]) == (200, "cerrado")


@requires_db
def test_abrir_cerrar_y_resolver_mercado_inexistente_devuelve_404(client):
    mid = uuid.uuid4()

    assert client.post(f"/internal/markets/{mid}/open", headers=headers()).status_code == 404
    assert client.post(f"/internal/markets/{mid}/close", headers=headers()).status_code == 404
    assert client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "VER"}).status_code == 404


@requires_db
def test_resolver_guarda_el_ganador_y_repetirlo_es_200(client, fabrica):
    mid = _crear(client, fabrica, status="abierto")
    client.post(f"/internal/markets/{mid}/close", headers=headers())

    r = client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "NOR"})
    repetido = client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "NOR"})

    assert r.status_code == 200, r.text
    assert (r.json()["winning_option"], r.json()["status"]) == ("NOR", "cerrado")
    assert repetido.status_code == 200


@requires_db
def test_resolver_con_otra_opcion_devuelve_409(client, fabrica):
    mid = _crear(client, fabrica)
    client.post(f"/internal/markets/{mid}/close", headers=headers())
    client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "NOR"})

    r = client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "VER"})

    assert r.status_code == 409


@requires_db
def test_resolver_opcion_inexistente_devuelve_422(client, fabrica):
    mid = _crear(client, fabrica)
    client.post(f"/internal/markets/{mid}/close", headers=headers())

    r = client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "HAM"})

    assert r.status_code == 422


@requires_db
def test_resolver_un_mercado_no_cerrado_devuelve_409(client, fabrica):
    abierto = _crear(client, fabrica, status="abierto")
    programado = _crear(client, fabrica)

    for mid in (abierto, programado):
        r = client.post(f"/internal/markets/{mid}/resolve", headers=headers(), json={"winning_option": "VER"})
        assert r.status_code == 409, r.text
        assert r.json()["detail"] == "El mercado no está cerrado"


@requires_db
def test_flujo_completo_deja_el_mercado_listo_para_liquidar(client, fabrica):
    mid = _crear(client, fabrica)

    for paso, cuerpo in (("open", None), ("close", None), ("resolve", {"winning_option": "VER"})):
        r = client.post(f"/internal/markets/{mid}/{paso}", headers=headers(), json=cuerpo)
        assert r.status_code == 200, r.text

    r = client.get("/internal/markets?status=cerrado&resuelto=true&limit=200", headers=headers())

    listo = next(m for m in r.json() if m["market_id"] == mid)
    assert (listo["status"], listo["winning_option"]) == ("cerrado", "VER")


@requires_db
def test_fechas_sin_zona_horaria_devuelven_422_y_con_zona_son_idempotentes(client, fabrica):
    cuerpo = _cuerpo(fabrica.evento().id)

    sin_zona = client.post("/internal/markets", headers=headers(), json={**cuerpo, "closes_at": "2030-01-01T10:00:00"})
    sin_zona_abre = client.post("/internal/markets", headers=headers(), json={**cuerpo, "opens_at": "2030-01-01T09:00:00"})
    con_zona = {**cuerpo, "closes_at": "2030-01-01T10:00:00Z"}
    primero = client.post("/internal/markets", headers=headers(), json=con_zona)
    reintento = client.post("/internal/markets", headers=headers(), json=con_zona)

    assert sin_zona.status_code == 422, sin_zona.text
    assert sin_zona_abre.status_code == 422, sin_zona_abre.text
    assert primero.status_code == 201, primero.text
    assert reintento.status_code == 200, reintento.text
