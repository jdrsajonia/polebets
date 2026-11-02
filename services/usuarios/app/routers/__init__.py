"""Routers de Usuarios.

    auth.py      POST /auth/login, POST /auth/logout, GET /auth/me   (publicas, via Gateway)
    internal.py  GET /internal/auth          (auth_request del Gateway, T2-01)
                 POST /internal/users        (registro orquestado, HU2-01)
                 GET  /internal/users/{id}   (pagina SSR, HU2-03)
"""
from app.routers import auth, internal

ROUTERS = [auth.router, internal.router_gateway, internal.router_servicio]
