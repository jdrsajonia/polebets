"""
Admin — Servicio real para HU2-18, HU2-19 y HU2-23.
Implementa la comunicación con RabbitMQ para consultas y simulaciones.
"""
import os
import uuid
import json
import pika
import time
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from typing import Dict, Any, Optional, Literal

app = FastAPI(title="Admin API")

# Configuración de RabbitMQ
RABBITMQ_URL = os.getenv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")

def get_rabbitmq_connection():
    """Crea una conexión a RabbitMQ."""
    params = pika.URLParameters(RABBITMQ_URL)
    return pika.BlockingConnection(params)

# --- Modelos de Petición ---
class SimulacionRequest(BaseModel):
    race_id: str
    minutos_anticipacion: int = Field(10, ge=1, le=120)

# HU2-23: Modelo de filtros para las consultas (mapeados al contrato de Pagos)
class FiltrosConsulta(BaseModel):
    evento_id: Optional[str] = None
    fecha_inicio: Optional[str] = None
    fecha_fin: Optional[str] = None
    tipo_apuesta: Optional[Literal["estatica", "dinamica"]] = None
    modo: Optional[Literal["torneo", "individual"]] = None

class ConsultaRequest(BaseModel):
    query: str
    params: FiltrosConsulta = FiltrosConsulta()

# --- Healthcheck ---
@app.get("/health")
def health():
    return {"estado": "ok", "servicio": "admin"}

# --- Utilidad: petición-respuesta con cola temporal ---
def rpc_call(queue: str, mensaje: dict, timeout: float = 10.0) -> dict:
    """
    Publica un mensaje en 'queue' y espera la respuesta en una cola temporal.
    Devuelve el body de la respuesta o lanza HTTPException si hay timeout.
    """
    conn = get_rabbitmq_connection()
    ch = conn.channel()
    try:
        # Declarar cola de destino
        ch.queue_declare(queue=queue, durable=True)

        # Cola temporal exclusiva para la respuesta
        result = ch.queue_declare(queue="", exclusive=True, auto_delete=True)
        reply_queue = result.method.queue

        correlation_id = str(uuid.uuid4())

        ch.basic_publish(
            exchange="",
            routing_key=queue,
            body=json.dumps(mensaje),
            properties=pika.BasicProperties(
                delivery_mode=2,
                reply_to=reply_queue,
                correlation_id=correlation_id,
            ),
        )

        # Esperar la respuesta
        start_time = time.time()
        while time.time() - start_time < timeout:
            method, properties, body = ch.basic_get(queue=reply_queue, auto_ack=True)
            if method:
                if properties.correlation_id == correlation_id:
                    return json.loads(body)
            time.sleep(0.05)

        raise HTTPException(status_code=504, detail=f"Timeout: {queue} no respondió en {timeout}s")
    finally:
        if conn and conn.is_open:
            conn.close()

# --- HU2-19: Iniciar Simulación (petición-respuesta) ---
@app.post("/simulacion")
def iniciar_simulacion(req: SimulacionRequest):
    mensaje = {
        "action": "start_simulation",
        "race_id": req.race_id,
        "minutos_anticipacion": req.minutos_anticipacion,
    }
    try:
        resp = rpc_call("ingestor.commands", mensaje, timeout=10.0)
        if resp.get("ok"):
            return {"status": "ok", "simulacion_id": resp.get("simulacion_id"), "hora_largada": resp.get("hora_largada")}
        else:
            error = resp.get("error", "error_desconocido")
            if error == "ya_hay_simulacion":
                raise HTTPException(status_code=409, detail="Ya hay una simulación programada o en curso")
            elif error == "carrera_no_descargada":
                raise HTTPException(status_code=404, detail="La carrera no está descargada")
            else:
                raise HTTPException(status_code=500, detail=f"Error en Ingestor: {error}")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error en RabbitMQ: {str(e)}")

# --- HU2-19: Listar carreras (petición-respuesta con Ingestor) ---
@app.get("/carreras")
def listar_carreras():
    mensaje = {"action": "list_races"}
    try:
        resp = rpc_call("ingestor.consultas", mensaje, timeout=10.0)
        if resp.get("ok"):
            return {"status": "ok", "carreras": resp.get("carreras", [])}
        else:
            raise HTTPException(status_code=500, detail=f"Error en Ingestor: {resp.get('error')}")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error en RabbitMQ: {str(e)}")

# --- HU2-18 y HU2-23: Consultas Financieras con Filtros (petición-respuesta) ---
@app.post("/consultas")
def ejecutar_consulta(req: ConsultaRequest):
    # Mapear filtros al contrato de Pagos
    filtros = {}
    if req.params.evento_id:
        filtros["event_id"] = req.params.evento_id
    if req.params.fecha_inicio:
        filtros["desde"] = req.params.fecha_inicio
    if req.params.fecha_fin:
        filtros["hasta"] = req.params.fecha_fin
    if req.params.tipo_apuesta:
        filtros["kind"] = req.params.tipo_apuesta
    # NOTA: 'modo' no se manda porque Pagos aún no lo acepta (acordar con Guillermo y Juan Diego)

    mensaje = {
        "tipo": "rentabilidad",  # Según el contrato de Pagos
        "filtros": filtros,
    }

    try:
        resp = rpc_call("admin.consultas", mensaje, timeout=10.0)
        if resp.get("ok"):
            return {"status": "ok", "data": resp.get("datos")}
        else:
            raise HTTPException(status_code=500, detail=f"Error en Pagos: {resp.get('error')}")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error en RabbitMQ: {str(e)}")