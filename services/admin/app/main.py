"""Admin — STUB base (T2-04).

Esqueleto mínimo del servicio Admin (HU2-18, HU2-19, Iván). Solo responde
/health para que el contenedor arranque. Es el ÚNICO servicio de lógica que se
publica al host (puerto 8003): la CLV del administrador (Rust) le habla a él.

Qué va aquí cuando se implemente el servicio real:
  - Login de admin contra Usuarios (solo rol admin, RF-21).
  - Publicar en RabbitMQ (Broker): órdenes de simulación (HU2-19) y consultas
    del administrador (HU2-18), con cola de respuesta y correlation_id.
  - Devolver a la CLI las respuestas; timeout de [10 s] en las consultas.

Puerto interno y publicado: 8003.
"""
from fastapi import FastAPI

app = FastAPI(title="Admin (stub)")


@app.get("/health")
def health() -> dict[str, str]:
    return {"estado": "ok", "servicio": "admin", "nota": "stub T2-04"}
