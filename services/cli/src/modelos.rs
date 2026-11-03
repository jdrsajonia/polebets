//! Respuestas de Admin (services/admin/README.md) tal como llegan en JSON.
//!
//! Los nombres de los campos son los del contrato; los datos de las consultas
//! los define Pagos (services/pagos/app/schemas/consultas.py).

use serde::Deserialize;
use serde_json::{Map, Value};

/// Usuario que devuelve `POST /login` (el de Usuarios).
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Usuario {
    pub id: String,
    pub nombre: String,
    pub email: String,
    pub rol: String,
}

/// Sesión del administrador: el token va en `Authorization: Bearer`.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Sesion {
    pub token: String,
    pub usuario: Usuario,
}

/// Cuerpo de `GET /carreras`.
#[derive(Debug, Clone, Deserialize)]
pub struct RespuestaCarreras {
    pub carreras: Vec<Carrera>,
}

/// Carrera ya descargada en MongoDB (la lista la da el Ingestor por el Broker).
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Carrera {
    pub carrera_id: String,
    pub nombre: String,
    #[serde(default)]
    pub circuito: String,
    pub temporada: Option<i64>,
    pub total_vueltas: Option<i64>,
    /// Largada histórica de la carrera real (no la de la simulación).
    pub hora_largada: Option<String>,
    #[serde(default)]
    pub estado: String,
    /// Cantidad de pilotos.
    #[serde(default)]
    pub pilotos: i64,
}

/// Cuerpo de `POST /simulacion` cuando se programó.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct SimulacionProgramada {
    pub simulacion_id: String,
    pub hora_largada: String,
}

/// Cuerpo de `POST /consultas`: los datos y qué filtros usó de verdad (HU2-23).
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Consulta<T> {
    pub tipo: String,
    #[serde(default)]
    pub filtros_aplicados: Map<String, Value>,
    #[serde(default)]
    pub filtros_no_aplicados: Vec<String>,
    pub data: T,
}

/// HU2-20: cuánto entra y cuánto sale.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct ResumenFinanciero {
    pub tokens_comprados: i64,
    pub cop_comprados: i64,
    pub apostado_individual: i64,
    pub pagado_individual: i64,
    pub inscripciones_cobradas: i64,
    pub inscripciones_devueltas: i64,
    pub premios_pagados: i64,
    pub ganancia_neta: i64,
    pub botin_en_juego: i64,
    pub apostado_pendiente: i64,
}

/// HU2-21: una fila por tipo de apuesta y plantilla.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct FilaPlantilla {
    pub kind: String,
    pub template: String,
    pub apuestas: i64,
    pub aciertos: i64,
    pub acierto_pct: f64,
    pub apostado: i64,
    pub pagado: i64,
    pub ganancia_neta: i64,
    pub multiplicador_promedio: f64,
    pub mercados_generados: i64,
}

/// HU2-21: rentabilidad por plantilla y el ranking de las N con más y menos ganancia.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Rentabilidad {
    pub filas: Vec<FilaPlantilla>,
    pub top_ganancia: Vec<FilaPlantilla>,
    pub peor_ganancia: Vec<FilaPlantilla>,
    pub total_apuestas: i64,
    pub total_apostado: i64,
    pub total_pagado: i64,
    pub ganancia_neta: i64,
}

/// HU2-22: volumen y ganancia en una hora del día (hora de Colombia).
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct FilaHora {
    pub hora: i64,
    pub volumen: i64,
    pub ganancia: i64,
}

/// HU2-22: volumen y ganancia en una vuelta (None = apuestas sin vuelta).
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct FilaVuelta {
    pub vuelta: Option<i64>,
    pub volumen: i64,
    pub ganancia: i64,
}

/// HU2-22: horarios y momento de carrera.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Horarios {
    pub por_hora: Vec<FilaHora>,
    pub por_vuelta: Vec<FilaVuelta>,
}

/// HU2-22: participación de un evento por modo.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct FilaEvento {
    pub event_id: String,
    pub race_key: String,
    pub starts_at: Option<String>,
    pub tournament_status: String,
    pub inscritos: i64,
    pub solo_individuales: i64,
    pub botin: i64,
    pub ganancia_torneo: i64,
    pub ganancia_individual: i64,
    pub apostado_pendiente: i64,
}

/// HU2-22: participación por modo, un renglón por evento.
#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Participacion {
    pub eventos: Vec<FilaEvento>,
}

#[cfg(test)]
mod pruebas {
    use super::*;

    // Respuestas copiadas de Admin con el stack levantado.
    #[test]
    fn lee_la_participacion_real() {
        let json = r#"{"status":"ok","tipo":"participacion","filtros_aplicados":{"modo":"torneo"},"filtros_no_aplicados":[],
            "data":{"eventos":[{"event_id":"b5a431bb-4131-4ace-a714-5790374b5294","race_key":"9165","starts_at":"2026-11-03T20:45:59.955000Z",
            "tournament_status":"cancelado","inscritos":0,"solo_individuales":0,"botin":0,"ganancia_torneo":0,"ganancia_individual":0,"apostado_pendiente":0}]}}"#;
        let c: Consulta<Participacion> = serde_json::from_str(json).unwrap();
        assert_eq!(c.tipo, "participacion");
        assert_eq!(c.filtros_aplicados["modo"], "torneo");
        assert_eq!(c.data.eventos[0].race_key, "9165");
    }

    #[test]
    fn lee_las_carreras_y_tolera_campos_faltantes() {
        let json = r#"{"status":"ok","carreras":[{"carrera_id":"9165","nombre":"Singapore 2023 - Race","circuito":"Singapore",
            "temporada":2023,"total_vueltas":62,"hora_largada":"2023-09-17T12:00:00Z","estado":"descargada","pilotos":19},
            {"carrera_id":"x","nombre":"Sin datos"}]}"#;
        let r: RespuestaCarreras = serde_json::from_str(json).unwrap();
        assert_eq!(r.carreras[0].total_vueltas, Some(62));
        assert_eq!(r.carreras[1].pilotos, 0);
    }

    #[test]
    fn lee_horarios_con_vuelta_nula() {
        let json = r#"{"tipo":"horarios","data":{"por_hora":[{"hora":15,"volumen":300,"ganancia":-50}],
            "por_vuelta":[{"vuelta":null,"volumen":10,"ganancia":10}]}}"#;
        let c: Consulta<Horarios> = serde_json::from_str(json).unwrap();
        assert_eq!(c.data.por_vuelta[0].vuelta, None);
        assert!(c.filtros_no_aplicados.is_empty());
    }
}
