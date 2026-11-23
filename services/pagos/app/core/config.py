"""Configuracion del servicio Pagos, leida de variables de entorno.

En Docker los valores llegan desde docker-compose.yml (que a su vez lee el
.env de la raiz). Fuera de Docker se puede crear un .env dentro de
services/pagos/ con las mismas variables.
"""
from functools import lru_cache

from pydantic import field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

LARGO_MINIMO_TOKEN = 32


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # ---- Servicio ----
    app_name: str = "Pagos"
    app_version: str = "0.4.0"

    # ---- PostgreSQL Pagos ----
    database_url: str = "postgresql+psycopg://arqui:arqui_pass@localhost:5432/pagos_db"
    db_connect_retries: int = 10
    db_connect_retry_seconds: float = 2.0

    # ---- Tokens de servicio para /internal/* (RNF-17, escenario 4) ----
    # Uno por cada servicio que llama a Pagos. Ninguno de los dos sirve contra
    # Usuarios ni contra el Motor. Vacios por defecto porque el consumidor AMQP
    # usa esta misma configuracion y no recibe llamadas HTTP: la app HTTP los
    # exige al arrancar (tokens_servicio_configurados).
    token_desde_orquestador: str = ""
    token_desde_motor: str = ""

    # ---- Economia del token (RF-05) ----
    cop_per_token: float = 1000.0

    # ---- Torneo multijugador (HU2-06), valores tentativos ----
    # buy_in, fichas y comision son los valores por defecto al crear un evento
    # (quedan guardados en events). min_players y min_wager_pct no estan en el
    # modelo de datos y se leen siempre de aqui.
    event_buy_in: int = 100  # PoleCoins
    event_initial_chips: int = 1000  # fichas de juego por inscrito (no son PoleCoins)
    event_commission_pct: int = 10  # comision de la casa sobre el botin
    event_min_players: int = 4  # con menos, el torneo se cancela y se devuelve el buy_in
    event_min_wager_pct: int = 50  # % de fichas que hay que apostar para cobrar (HU2-12)

    @field_validator("token_desde_orquestador", "token_desde_motor")
    @classmethod
    def _token_largo(cls, value: str) -> str:
        """Vacio se permite (consumidor); si viene, con el largo minimo."""
        value = value.strip()
        if value and len(value) < LARGO_MINIMO_TOKEN:
            raise ValueError(f"los tokens de servicio deben tener al menos {LARGO_MINIMO_TOKEN} caracteres")
        return value

    def tokens_servicio_configurados(self) -> None:
        """Lanza ValueError si falta alguno de los dos tokens o si son iguales.

        Si fueran iguales, el Motor podria hacerse pasar por el Orquestador
        (y al reves): otra vez un token compartido.
        """
        faltan = [
            nombre
            for nombre, valor in (
                ("TOKEN_DESDE_ORQUESTADOR", self.token_desde_orquestador),
                ("TOKEN_DESDE_MOTOR", self.token_desde_motor),
            )
            if not valor
        ]
        if faltan:
            raise ValueError(f"faltan variables de entorno obligatorias: {', '.join(faltan)}")
        if self.token_desde_orquestador == self.token_desde_motor:
            raise ValueError("TOKEN_DESDE_ORQUESTADOR y TOKEN_DESDE_MOTOR tienen el mismo valor: cada servicio necesita su propio token")


@lru_cache
def get_settings() -> Settings:
    """Lee la configuracion una sola vez."""
    return Settings()


settings = get_settings()
