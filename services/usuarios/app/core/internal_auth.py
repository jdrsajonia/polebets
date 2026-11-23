"""Token de servicio para las rutas /internal/* que llama el Orquestador (RNF-17).

Header X-Internal-Service-Token. Desde el escenario 4 cada par de servicios
tiene su propio token: Usuarios solo acepta TOKEN_DESDE_ORQUESTADOR, que no
tienen ni el Motor ni Pagos. La comparacion es en tiempo constante para no
filtrar el token por tiempos.
"""
import hmac

from app.config import settings


def token_servicio_valido(token: str | None) -> bool:
    """True si el token es el del Orquestador."""
    if not token:
        return False
    return hmac.compare_digest(token.encode("utf-8"), settings.token_desde_orquestador.encode("utf-8"))
