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
    # cookie "session"). Desde el escenario 4 solo Usuarios tiene JWT_SECRET.
    jwt_secret: str
    jwt_algorithm: str = "HS256"
    jwt_expire_minutes: int = 120
    session_cookie_name: str = "session"
    # True solo con HTTPS. En la demo local (http://localhost) va en False.
    session_cookie_secure: bool = False

    # ---- Token de servicio para /internal/* (RNF-17, escenario 4) ----
    # El unico servicio que llama a Usuarios con token es el Orquestador, asi
    # que solo se acepta su token. El Motor y Pagos no lo tienen.
    token_desde_orquestador: str

    # ---- Identidad firmada (escenario 4, T3-05) ----
    # Ruta de la llave privada Ed25519 (scripts/generar-llaves-identidad.sh). En
    # el compose es un secret montado en /run/secrets. Solo Usuarios la tiene.
    identidad_llave_privada: str
    # Vida del token de identidad. Corta a proposito: si alguien lo copia, deja
    # de servir enseguida. El Gateway pide uno nuevo en cada peticion.
    identidad_ttl_segundos: int = 60

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

    @field_validator("identidad_ttl_segundos")
    @classmethod
    def _ttl_positivo(cls, valor: int) -> int:
        if valor <= 0:
            raise ValueError("IDENTIDAD_TTL_SEGUNDOS debe ser mayor que 0")
        return valor

    @field_validator("token_desde_orquestador")
    @classmethod
    def _token_largo(cls, valor: str) -> str:
        """Rechaza tokens cortos: se adivinan mas facil."""
        if len(valor) < LARGO_MINIMO_TOKEN:
            raise ValueError(f"TOKEN_DESDE_ORQUESTADOR debe tener al menos {LARGO_MINIMO_TOKEN} caracteres")
        return valor


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
