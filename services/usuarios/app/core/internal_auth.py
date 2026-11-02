"""Token de servicio para las rutas /internal/* que llama el Orquestador (RNF-17).

Mismo esquema que Pagos: header X-Internal-Service-Token con INTERNAL_API_TOKEN.
La comparacion es en tiempo constante para no filtrar el token por tiempos.
"""
import hmac

from app.config import settings


def token_servicio_valido(token: str | None) -> bool:
    if not token:
        return False
    return hmac.compare_digest(token.encode("utf-8"), settings.internal_api_token.encode("utf-8"))
