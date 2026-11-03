"""
Admin — HU2-18 (Broker), HU2-19 (iniciar simulación) y HU2-23 (filtros).

Atiende a la CLI por REST y habla con Pagos y el Ingestor solo por el Broker
(RabbitMQ), en petición-respuesta: cada petición declara una cola temporal
exclusiva, la pasa en `reply_to` y espera la respuesta con su `correlation_id`
hasta [10 s] (RF-30). Así dos consultas a la vez nunca se cruzan.

Colas (contrato «Colas y mensajes del Broker»):
  - admin.consultas     → Pagos   {"tipo": "rentabilidad" | "resumen_financiero" | "horarios" | "participacion", "filtros": {...}}
  - ingestor.commands   → Ingestor {"action": "start_simulation", "race_id", "minutos_anticipacion"}
  - ingestor.consultas  → Ingestor {"action": "list_races"}
"""
import json
import logging
import os
import time
import uuid
from datetime import datetime
from typing import Any, Literal, Optional

import pika
from pika.exceptions import AMQPError
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, ConfigDict, Field

log = logging.getLogger("admin")

COLA_CONSULTAS_PAGOS = "admin.consultas"
COLA_ORDENES_INGESTOR = "ingestor.commands"
COLA_CONSULTAS_INGESTOR = "ingestor.consultas"
TIMEOUT_S = 10.0


def _url_broker() -> str:
    # Obligatoria, sin valor por defecto: si falta, Admin no arranca.
    url = os.environ.get("RABBITMQ_URL", "").strip()
    if not url:
        raise RuntimeError("falta RABBITMQ_URL para Admin")
    return url


RABBITMQ_URL = _url_broker()

app = FastAPI(title="Admin API")


# --- Modelos de petición ---
class SimulacionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    race_id: str = Field(min_length=1)
    minutos_anticipacion: int = Field(10, ge=1, le=120)


class FiltrosConsulta(BaseModel):
    """HU2-23 (RF-29). Todos opcionales; se traducen al contrato de Pagos."""

    model_config = ConfigDict(extra="forbid")

    evento_id: Optional[uuid.UUID] = None
    fecha_inicio: Optional[datetime] = None
    fecha_fin: Optional[datetime] = None
    tipo_apuesta: Optional[Literal["estatica", "dinamica"]] = None
    plantilla: Optional[str] = Field(None, max_length=100)
    top_n: Optional[int] = Field(None, ge=1, le=100)
    modo: Optional[Literal["torneo", "individual"]] = None


class ConsultaRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: Literal["rentabilidad", "resumen_financiero", "horarios", "participacion"] = "rentabilidad"
    params: FiltrosConsulta = Field(default_factory=FiltrosConsulta)


# Filtros de Admin → nombre en el contrato de Pagos (app/schemas/consultas.py).
_MAPA_FILTROS = {
    "evento_id": "event_id",
    "fecha_inicio": "desde",
    "fecha_fin": "hasta",
    "tipo_apuesta": "kind",
    "plantilla": "template",
    "top_n": "top_n",
    "modo": "modo",
}

# Filtros que aplica cada consulta. Los demás no se mandan a Pagos y vuelven en
# filtros_no_aplicados: la rentabilidad aún no filtra por modo, y plantilla y
# top_n no tienen sentido en el resumen financiero (HU2-20) ni en horarios y
# participacion (HU2-22).
_FILTROS_POR_CONSULTA = {
    "rentabilidad": {"evento_id", "fecha_inicio", "fecha_fin", "tipo_apuesta", "plantilla", "top_n"},
    "resumen_financiero": {"evento_id", "fecha_inicio", "fecha_fin", "tipo_apuesta", "modo"},
    "horarios": {"evento_id", "fecha_inicio", "fecha_fin", "tipo_apuesta", "modo"},
    "participacion": {"evento_id", "fecha_inicio", "fecha_fin", "tipo_apuesta", "modo"},
}


def filtros_para_pagos(query: str, f: FiltrosConsulta) -> dict[str, Any]:
    datos = f.model_dump(mode="json", exclude_none=True)
    return {_MAPA_FILTROS[k]: v for k, v in datos.items() if k in _FILTROS_POR_CONSULTA[query]}


# --- Petición-respuesta por el Broker ---
class SinRespuesta(Exception):
    """No llegó respuesta antes del timeout."""


def rpc_call(cola: str, mensaje: dict, timeout: float = TIMEOUT_S) -> dict:
    """Publica `mensaje` en `cola` y espera la respuesta en una cola temporal."""
    conexion = pika.BlockingConnection(pika.URLParameters(RABBITMQ_URL))
    try:
        canal = conexion.channel()
        canal.queue_declare(queue=cola, durable=True)
        # Cola propia de esta petición: la borra el Broker al cerrar la conexión.
        reply_to = canal.queue_declare(queue="", exclusive=True, auto_delete=True).method.queue
        correlation_id = str(uuid.uuid4())
        respuesta: dict[str, Any] = {}

        def al_llegar(ch, method, props, cuerpo):
            if props.correlation_id == correlation_id:
                respuesta["cuerpo"] = cuerpo

        canal.basic_consume(queue=reply_to, on_message_callback=al_llegar, auto_ack=True)
        canal.basic_publish(
            exchange="",
            routing_key=cola,
            body=json.dumps(mensaje),
            properties=pika.BasicProperties(
                content_type="application/json",
                delivery_mode=2,
                reply_to=reply_to,
                correlation_id=correlation_id,
                # Si nadie la toma a tiempo, el Broker la descarta: una orden
                # vieja no debe programar una simulación después de que la
                # CLI ya recibió "tardó demasiado".
                expiration=str(int(timeout * 1000)),
            ),
        )
        # process_data_events vuelve apenas procesa algún evento, no al vencer
        # time_limit: por eso se repite hasta la respuesta o el plazo.
        plazo = time.monotonic() + timeout
        while "cuerpo" not in respuesta:
            restante = plazo - time.monotonic()
            if restante <= 0:
                break
            conexion.process_data_events(time_limit=restante)
        if "cuerpo" not in respuesta:
            raise SinRespuesta()
        return json.loads(respuesta["cuerpo"])
    finally:
        if conexion.is_open:
            conexion.close()


def _pedir(cola: str, mensaje: dict, destino: str) -> dict:
    try:
        return rpc_call(cola, mensaje)
    except SinRespuesta:
        raise HTTPException(status_code=504, detail=f"La consulta tardó demasiado: {destino} no respondió en {TIMEOUT_S:.0f} s")
    except (AMQPError, OSError) as e:
        log.warning("Broker no disponible: %s", e)
        raise HTTPException(status_code=503, detail="Broker no disponible")


# --- Healthcheck ---
@app.get("/health")
def health():
    return {"estado": "ok", "servicio": "admin"}


# --- HU2-19: lista de carreras descargadas (las pide al Ingestor) ---
@app.get("/carreras")
def listar_carreras():
    resp = _pedir(COLA_CONSULTAS_INGESTOR, {"action": "list_races"}, "el Ingestor")
    if not resp.get("ok"):
        raise HTTPException(status_code=502, detail=f"Error en el Ingestor: {resp.get('error')}")
    return {"status": "ok", "carreras": resp.get("carreras", [])}


# --- HU2-19: iniciar simulación ---
@app.post("/simulacion")
def iniciar_simulacion(req: SimulacionRequest):
    mensaje = {
        "action": "start_simulation",
        "race_id": req.race_id,
        "minutos_anticipacion": req.minutos_anticipacion,
    }
    resp = _pedir(COLA_ORDENES_INGESTOR, mensaje, "el Ingestor")
    if resp.get("ok"):
        return {"status": "ok", "simulacion_id": resp.get("simulacion_id"), "hora_largada": resp.get("hora_largada")}
    error = resp.get("error", "error_desconocido")
    if error == "ya_hay_simulacion":
        raise HTTPException(status_code=409, detail="Ya hay una simulación programada o en curso")
    if error == "carrera_no_descargada":
        raise HTTPException(status_code=404, detail="La carrera no está descargada")
    if error == "orden_invalida":
        raise HTTPException(status_code=422, detail="Orden de simulación inválida")
    raise HTTPException(status_code=502, detail=f"Error en el Ingestor: {error}")


# --- HU2-18 y HU2-23: consultas a Pagos con filtros ---
@app.post("/consultas")
def ejecutar_consulta(req: ConsultaRequest):
    filtros = filtros_para_pagos(req.query, req.params)
    resp = _pedir(COLA_CONSULTAS_PAGOS, {"tipo": req.query, "filtros": filtros}, "Pagos")
    if not resp.get("ok"):
        raise HTTPException(status_code=502, detail=f"Error en Pagos: {resp.get('error')}")
    # HU2-23: la CLI muestra qué filtros quedaron activos.
    pedidos = req.params.model_dump(mode="json", exclude_none=True)
    aplica = _FILTROS_POR_CONSULTA[req.query]
    return {
        "status": "ok",
        "tipo": resp.get("tipo"),
        "filtros_aplicados": {k: v for k, v in pedidos.items() if k in aplica},
        "filtros_no_aplicados": [k for k in pedidos if k not in aplica],
        "data": resp.get("datos"),
    }
