"""Configuracion del servicio Usuarios, leida de variables de entorno (ver docker-compose.yml).

Las variables sin valor por defecto son obligatorias: si falta alguna, el
servicio no arranca. Asi nunca corre con un secreto de ejemplo escrito en el
codigo.
"""
from functools import lru_cache

from pydantic import field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# Mismo minimo que exigen Pagos, el Motor y el Orquestador.
LARGO_MINIMO_TOKEN = 32


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # ---- PostgreSQL Usuarios ----
    database_url: str = "postgresql+psycopg://arqui:arqui_pass@localhost:5432/usuarios_db"
    db_connect_retries: int = 10
    db_connect_retry_seconds: float = 2.0

    # ---- Sesion (HU2-02) ----
    # El JWT es el mismo contrato del prototipo 1 (HS256, claims sub/rol/exp,
    # cookie "session"). Se mantiene igual para que el Motor, que todavia
    # valida el JWT del WebSocket por su cuenta, siga funcionando.
    jwt_secret: str
    jwt_algorithm: str = "HS256"
    jwt_expire_minutes: int = 120
    session_cookie_name: str = "session"
    # True solo con HTTPS. En la demo local (http://localhost) va en False.
    session_cookie_secure: bool = False

    # ---- Token de servicio para /internal/* (RNF-17) ----
    internal_api_token: str

    # ---- Administrador inicial (RF-21) ----
    # Opcional: si falta el correo o la contraseña, no se crea y se avisa en el log.
    admin_email: str = ""
    admin_password: str = ""
    admin_name: str = "Administrador"

    @field_validator("jwt_secret")
    @classmethod
    def _secreto_no_vacio(cls, valor: str) -> str:
        if not valor.strip():
            raise ValueError("JWT_SECRET no puede estar vacío")
        return valor

    @field_validator("internal_api_token")
    @classmethod
    def _token_largo(cls, valor: str) -> str:
        if len(valor) < LARGO_MINIMO_TOKEN:
            raise ValueError(f"INTERNAL_API_TOKEN debe tener al menos {LARGO_MINIMO_TOKEN} caracteres")
        return valor


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
