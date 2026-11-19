"""Escenario 2 (T3-03): Admin no publica el mapa de su API."""
import pytest


@pytest.mark.parametrize("ruta", ["/docs", "/redoc", "/openapi.json", "/docs/oauth2-redirect"])
def test_la_documentacion_de_la_api_no_existe(client, ruta):
    assert client.get(ruta).status_code == 404
