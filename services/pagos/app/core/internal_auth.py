"""Autenticacion de las rutas internas de servicio (escenario 4, T3-05).

Pagos acepta dos tokens, uno por cada servicio que lo llama: el del Orquestador
y el del Motor. Cada token solo lo conocen Pagos y ese servicio, asi que no
sirve para llamar a Usuarios ni al Motor. La comparacion es en tiempo
constante para no filtrar el token por tiempos.
"""
import hmac

from app.core.config import settings


def servicio_que_llama(token: str | None) -> str | None:
    """Devuelve "orquestador" o "motor" segun el token, o None si no es ninguno."""
    if not token:
        return None
    recibido = token.encode("utf-8")
    encontrado = None
    # Se comparan los dos siempre (sin cortar en el primero) para que el
    # tiempo no dependa de cual coincide.
    for nombre, esperado in (("orquestador", settings.token_desde_orquestador), ("motor", settings.token_desde_motor)):
        if esperado and hmac.compare_digest(recibido, esperado.encode("utf-8")):
            encontrado = nombre
    return encontrado


def token_servicio_valido(token: str | None) -> bool:
    """True si el token es el del Orquestador o el del Motor."""
    return servicio_que_llama(token) is not None
