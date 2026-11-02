"""Cookie de sesion (HU2-02, RNF-06). Un solo lugar decide como se escribe y se borra.

Por que estos atributos:

- HttpOnly: el JavaScript del front NO puede leer el token, asi un script
  inyectado (XSS) no se roba la sesion.
- SameSite=Lax: el navegador no envia la cookie en peticiones que vienen de
  otro sitio (protege contra CSRF). Con el Gateway todo es el mismo origen
  (http://localhost), asi que la cookie viaja sola a /api/... y al WebSocket.
- Secure: solo por HTTPS. En la demo local va en False (SESSION_COOKIE_SECURE).
- Max-Age igual a la vida del JWT: el navegador no guarda una sesion muerta.
"""
from fastapi import Response

from app.config import settings

# Deben ser identicos al crear y al borrar; si no, el navegador trata el
# borrado como otra cookie y la sesion sigue viva.
_ATRIBUTOS_COMUNES = {"path": "/", "httponly": True, "samesite": "lax"}


def poner_cookie_sesion(response: Response, token: str) -> None:
    response.set_cookie(
        key=settings.session_cookie_name,
        value=token,
        max_age=settings.jwt_expire_minutes * 60,
        secure=settings.session_cookie_secure,
        **_ATRIBUTOS_COMUNES,
    )


def borrar_cookie_sesion(response: Response) -> None:
    response.delete_cookie(
        key=settings.session_cookie_name,
        secure=settings.session_cookie_secure,
        **_ATRIBUTOS_COMUNES,
    )


def sin_cache(response: Response) -> None:
    """Respuestas con datos de sesion: que ni proxies ni el navegador las guarden."""
    response.headers["Cache-Control"] = "no-store"
