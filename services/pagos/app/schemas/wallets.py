"""Contratos internos de la billetera (ver documentation/orquestador-api.md, §5)."""
import uuid

from pydantic import BaseModel, ConfigDict


class WalletIn(BaseModel):
    model_config = ConfigDict(extra="forbid")

    user_id: uuid.UUID


class WalletOut(BaseModel):
    user_id: uuid.UUID
    balance: int  # tokens disponibles
    cop_por_token: int  # tasa fija (COP_PER_TOKEN)
