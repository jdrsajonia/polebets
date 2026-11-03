//! Corre una `Peticion` de la app contra Admin y devuelve sus `Datos`.

use crate::api::{ClienteAdmin, ErrorApi};
use crate::app::{Datos, Panel, Peticion};

/// Hace la llamada a Admin que corresponde a la petición.
pub async fn resolver(cliente: &ClienteAdmin, peticion: Peticion) -> Result<Datos, ErrorApi> {
    match peticion {
        Peticion::Login { email, password } => cliente.login(&email, &password).await.map(Datos::Sesion),
        Peticion::Carreras { token } => cliente.carreras(&token).await.map(Datos::Carreras),
        Peticion::Programar {
            token,
            race_id,
            minutos,
        } => cliente
            .programar(&token, &race_id, minutos)
            .await
            .map(Datos::Programada),
        Peticion::Consulta { token, panel, params } => {
            let query = panel.query().unwrap_or_default();
            match panel {
                Panel::Simulacion => cliente.carreras(&token).await.map(Datos::Carreras),
                Panel::Resumen => cliente.consultar(&token, query, params).await.map(Datos::Resumen),
                Panel::Rentabilidad => cliente.consultar(&token, query, params).await.map(Datos::Rentabilidad),
                Panel::Horarios => cliente.consultar(&token, query, params).await.map(Datos::Horarios),
                Panel::Participacion => cliente.consultar(&token, query, params).await.map(Datos::Participacion),
            }
        }
    }
}
