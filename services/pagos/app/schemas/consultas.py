"""HU2-20, HU2-21 y HU2-22: contrato de las consultas del administrador por el Broker.

Lo define esta HU (el consumidor de Pagos) porque HU2-18 (Admin/Broker, Iván)
aún no existe. Cuando exista, Admin publica mensajes con esta forma y lee las
respuestas con el mismo `correlation_id`. Si Iván elige otro formato, se ajusta
solo aquí y en app/consumer.py; la consulta SQL (services/rentabilidad.py) no
cambia.

Mensaje de consulta (lo publica Admin en la cola de consultas):

    {
      "tipo": "rentabilidad",
      "filtros": {"event_id": "...", "desde": "...", "hasta": "...",
                  "kind": "estatica", "template": "ganador", "top_n": 5}
    }

El `correlation_id` y la cola de respuesta (`reply_to`) viajan en las
PROPIEDADES del mensaje AMQP, no en el cuerpo (es lo estándar en el patrón
petición-respuesta de RabbitMQ).

Mensaje de respuesta (lo publica Pagos en `reply_to`, con el mismo
`correlation_id`):

    {"tipo": "rentabilidad", "ok": true, "datos": {...}}
    {"tipo": "rentabilidad", "ok": false, "error": "kind_invalido"}
"""
import uuid
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

# Tipos de consulta que entiende el consumidor: "rentabilidad" (HU2-21),
# "resumen_financiero" (HU2-20), "horarios" y "participacion" (HU2-22).
TIPO_RENTABILIDAD = "rentabilidad"
TIPO_RESUMEN_FINANCIERO = "resumen_financiero"
TIPO_HORARIOS = "horarios"
TIPO_PARTICIPACION = "participacion"


class FiltrosConsulta(BaseModel):
    """Filtros de RF-29. Todos opcionales."""

    model_config = ConfigDict(extra="forbid")

    event_id: uuid.UUID | None = None
    desde: datetime | None = None
    hasta: datetime | None = None
    kind: Literal["estatica", "dinamica"] | None = None
    template: str | None = Field(default=None, max_length=100)
    top_n: int = Field(default=5, ge=1, le=100)


class ConsultaRentabilidad(BaseModel):
    """Cuerpo del mensaje de consulta que publica Admin."""

    model_config = ConfigDict(extra="forbid")

    tipo: Literal["rentabilidad"] = TIPO_RENTABILIDAD
    filtros: FiltrosConsulta = Field(default_factory=FiltrosConsulta)


class FilaPlantillaOut(BaseModel):
    kind: str
    template: str
    apuestas: int
    aciertos: int
    acierto_pct: float
    apostado: int
    pagado: int
    ganancia_neta: int
    multiplicador_promedio: float
    mercados_generados: int


class DatosRentabilidad(BaseModel):
    filas: list[FilaPlantillaOut]
    top_ganancia: list[FilaPlantillaOut]
    peor_ganancia: list[FilaPlantillaOut]
    total_apuestas: int
    total_apostado: int
    total_pagado: int
    ganancia_neta: int


class FiltrosResumen(BaseModel):
    """Filtros del resumen financiero (HU2-20); los usan tambien horarios y
    participacion (HU2-22). Todos opcionales.

    RF-29 pide filtrar todas las consultas por evento, fechas, tipo de apuesta
    y modo. Plantilla y top_n son propios de la rentabilidad: si llegan, se
    rechazan como cualquier campo desconocido.
    """

    model_config = ConfigDict(extra="forbid")

    event_id: uuid.UUID | None = None
    desde: datetime | None = None
    hasta: datetime | None = None
    kind: Literal["estatica", "dinamica"] | None = None
    modo: Literal["individual", "torneo"] | None = None


class ConsultaResumenFinanciero(BaseModel):
    """Cuerpo del mensaje de consulta del resumen financiero (HU2-20)."""

    model_config = ConfigDict(extra="forbid")

    tipo: Literal["resumen_financiero"] = TIPO_RESUMEN_FINANCIERO
    filtros: FiltrosResumen = Field(default_factory=FiltrosResumen)


class DatosResumenFinanciero(BaseModel):
    tokens_comprados: int
    cop_comprados: int
    apostado_individual: int  # incluye las apuestas aun sin resolver
    pagado_individual: int
    inscripciones_cobradas: int
    inscripciones_devueltas: int
    premios_pagados: int
    ganancia_neta: int
    botin_en_juego: int
    apostado_pendiente: int


class ConsultaHorarios(BaseModel):
    """Cuerpo del mensaje de consulta de horarios (HU2-22, RF-27)."""

    model_config = ConfigDict(extra="forbid")

    tipo: Literal["horarios"] = TIPO_HORARIOS
    filtros: FiltrosResumen = Field(default_factory=FiltrosResumen)


class FilaHoraOut(BaseModel):
    hora: int  # 0-23, hora de Colombia
    volumen: int  # tokens apostados en individual
    ganancia: int


class FilaVueltaOut(BaseModel):
    vuelta: int | None  # None = apuestas sin vuelta
    volumen: int
    ganancia: int


class DatosHorarios(BaseModel):
    por_hora: list[FilaHoraOut]
    por_vuelta: list[FilaVueltaOut]


class ConsultaParticipacion(BaseModel):
    """Cuerpo del mensaje de consulta de participacion (HU2-22, RF-28)."""

    model_config = ConfigDict(extra="forbid")

    tipo: Literal["participacion"] = TIPO_PARTICIPACION
    filtros: FiltrosResumen = Field(default_factory=FiltrosResumen)


class FilaEventoOut(BaseModel):
    event_id: uuid.UUID
    race_key: str
    starts_at: datetime
    tournament_status: str
    inscritos: int
    solo_individuales: int
    botin: int
    ganancia_torneo: int
    ganancia_individual: int
    apostado_pendiente: int


class DatosParticipacion(BaseModel):
    eventos: list[FilaEventoOut]


class RespuestaConsulta(BaseModel):
    """Lo que Pagos publica en la cola de respuesta."""

    tipo: str
    ok: bool
    datos: DatosRentabilidad | DatosResumenFinanciero | DatosHorarios | DatosParticipacion | None = None
    error: str | None = None
