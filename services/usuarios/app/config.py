"""Configuracion del servicio Usuarios, leida de variables de entorno (ver docker-compose.yml)."""
from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # ---- PostgreSQL Usuarios ----
    database_url: str = "postgresql+psycopg://arqui:arqui_pass@localhost:5432/usuarios_db"
    db_connect_retries: int = 10
    db_connect_retry_seconds: float = 2.0


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
