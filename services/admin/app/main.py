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
from pydantic import BaseModel
from typing import Dict, Any, Optional

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
    minutos_anticipacion: int = 10

# HU2-23: Modelo de filtros para las consultas
class FiltrosConsulta(BaseModel):
    evento_id: Optional[str] = None
    fecha_inicio: Optional[str] = None
    fecha_fin: Optional[str] = None
    tipo_apuesta: Optional[str] = None  # "estatica" | "dinamica"
    modo: Optional[str] = None          # "torneo" | "individual"

class ConsultaRequest(BaseModel):
    query: str
    params: FiltrosConsulta = FiltrosConsulta()

# --- Healthcheck ---
@app.get("/health")
def health():
    return {"estado": "ok", "servicio": "admin"}

# --- HU2-19: Iniciar Simulación ---
@app.post("/simulacion")
def iniciar_simulacion(req: SimulacionRequest):
    try:
        conn = get_rabbitmq_connection()
        ch = conn.channel()
        ch.queue_declare(queue="ingestor.commands", durable=True)
        
        mensaje = {
            "action": "start_simulation",
            "race_id": req.race_id,
            "minutos_anticipacion": req.minutos_anticipacion
        }
        ch.basic_publish(
            exchange="",
            routing_key="ingestor.commands",
            body=json.dumps(mensaje),
            properties=pika.BasicProperties(delivery_mode=2)
        )
        conn.close()
        return {"status": "ok", "mensaje": f"Simulación de {req.race_id} programada"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error publicando en RabbitMQ: {str(e)}")

# --- HU2-18 y HU2-23: Consultas Financieras con Filtros (Asíncrono con timeout) ---
@app.post("/consultas")
def ejecutar_consulta(req: ConsultaRequest):
    correlation_id = str(uuid.uuid4())
    conn = None
    try:
        conn = get_rabbitmq_connection()
        ch = conn.channel()
        
        # 1. Declarar colas
        ch.queue_declare(queue="pagos.consultas", durable=True)
        ch.queue_declare(queue="admin.respuestas", durable=True)
        
        # 2. Limpiamos los filtros que vengan en None para no enviar basura
        filtros_activos = {k: v for k, v in req.params.dict().items() if v is not None}
        
        # 3. Publicar consulta en Pagos (incluyendo los filtros de HU2-23)
        mensaje = {
            "correlation_id": correlation_id,
            "query": req.query,
            "params": filtros_activos
        }
        ch.basic_publish(
            exchange="",
            routing_key="pagos.consultas",
            body=json.dumps(mensaje),
            properties=pika.BasicProperties(
                delivery_mode=2,
                reply_to="admin.respuestas",
                correlation_id=correlation_id
            )
        )
        
        # 4. Esperar la respuesta (Bloqueante con timeout de 10s)
        start_time = time.time()
        while time.time() - start_time < 10.0:
            method, properties, body = ch.basic_get(queue="admin.respuestas", auto_ack=True)
            if method:
                respuesta = json.loads(body)
                if respuesta.get("correlation_id") == correlation_id:
                    return {"status": "ok", "data": respuesta.get("data")}
            time.sleep(0.1)
        
        # Si llegamos aquí, se cumplió el timeout
        raise HTTPException(status_code=504, detail="Timeout: Pagos no respondió en 10s")
        
    except Exception as e:
        if isinstance(e, HTTPException):
            raise e
        raise HTTPException(status_code=500, detail=f"Error en RabbitMQ: {str(e)}")
    finally:
        if conn and conn.is_open:
            conn.close()