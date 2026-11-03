//! Cliente REST de Admin (puerto 8003). Es la única puerta de la CLI: nunca
//! habla con Pagos, el Ingestor ni OpenF1 (Vista C&C).
//!
//! Cada error se traduce a `ErrorApi` con un mensaje listo para mostrar.

use std::time::Duration;

use reqwest::{Client, RequestBuilder};
use serde::de::DeserializeOwned;
use serde_json::{json, Map, Value};

use crate::modelos::{Carrera, Consulta, RespuestaCarreras, Sesion, SimulacionProgramada};

/// Admin corta a los 10 s con un 504 (RF-30). La CLI espera un poco más para
/// recibir ese 504 con su mensaje; si ni eso llega, avisa por su cuenta.
pub const TIMEOUT: Duration = Duration::from_secs(12);

/// Lo que puede salir mal al hablar con Admin.
#[derive(Debug, Clone, PartialEq)]
pub enum ErrorApi {
    /// 401: credenciales malas, sin token o sesión vencida.
    SinSesion(String),
    /// 403: la cuenta no es de administrador.
    Prohibido(String),
    /// 504 de Admin o timeout de la CLI (RF-30).
    TardoDemasiado(String),
    /// Admin no responde (apagado, URL mala).
    SinConexion(String),
    /// Cualquier otra respuesta de error (404, 409, 422, 502, 503...).
    Servidor { estado: u16, mensaje: String },
}

impl ErrorApi {
    /// Mensaje en español para la pantalla.
    pub fn mensaje(&self) -> String {
        match self {
            ErrorApi::SinSesion(m)
            | ErrorApi::Prohibido(m)
            | ErrorApi::TardoDemasiado(m)
            | ErrorApi::SinConexion(m) => m.clone(),
            ErrorApi::Servidor { mensaje, .. } => mensaje.clone(),
        }
    }
}

/// Saca el mensaje de `{"detail": ...}` (formato de FastAPI). Si es una lista
/// de errores de validación (422), nombra el primer campo inválido.
pub fn detalle(cuerpo: &str) -> Option<String> {
    let valor: Value = serde_json::from_str(cuerpo).ok()?;
    match valor.get("detail")? {
        Value::String(texto) => Some(texto.clone()),
        Value::Array(errores) => {
            let campo = errores
                .first()?
                .get("loc")?
                .as_array()?
                .last()?
                .as_str()
                .unwrap_or("?")
                .to_string();
            Some(format!("Dato inválido en «{campo}»"))
        }
        _ => None,
    }
}

/// Convierte una respuesta de error de Admin en `ErrorApi`.
pub fn error_de_respuesta(estado: u16, cuerpo: &str) -> ErrorApi {
    let mensaje = detalle(cuerpo).unwrap_or_else(|| format!("Admin respondió {estado}"));
    match estado {
        401 => ErrorApi::SinSesion(mensaje),
        403 => ErrorApi::Prohibido(mensaje),
        504 => ErrorApi::TardoDemasiado(mensaje),
        _ => ErrorApi::Servidor { estado, mensaje },
    }
}

/// Cliente de Admin. Clonarlo es barato: comparte el pool de conexiones.
#[derive(Clone)]
pub struct ClienteAdmin {
    http: Client,
    base: String,
}

impl ClienteAdmin {
    /// Crea el cliente para la URL de Admin (p. ej. http://localhost:8003).
    pub fn nuevo(base: &str) -> Self {
        let http = Client::builder()
            .timeout(TIMEOUT)
            .connect_timeout(Duration::from_secs(3))
            .build()
            .expect("cliente HTTP");
        ClienteAdmin {
            http,
            base: base.trim_end_matches('/').to_string(),
        }
    }

    /// URL de Admin que usa este cliente.
    pub fn base(&self) -> &str {
        &self.base
    }

    /// RF-21: inicia sesión; Admin solo entrega el token a un administrador.
    pub async fn login(&self, email: &str, password: &str) -> Result<Sesion, ErrorApi> {
        let req = self
            .http
            .post(self.url("/login"))
            .json(&json!({"email": email, "password": password}));
        self.enviar(req).await
    }

    /// HU2-19: carreras ya descargadas en MongoDB.
    pub async fn carreras(&self, token: &str) -> Result<Vec<Carrera>, ErrorApi> {
        let req = self.http.get(self.url("/carreras")).bearer_auth(token);
        self.enviar::<RespuestaCarreras>(req).await.map(|r| r.carreras)
    }

    /// HU2-19: programa la simulación de una carrera con sus minutos de anticipación.
    pub async fn programar(&self, token: &str, race_id: &str, minutos: u32) -> Result<SimulacionProgramada, ErrorApi> {
        let cuerpo = json!({"race_id": race_id, "minutos_anticipacion": minutos});
        let req = self.http.post(self.url("/simulacion")).bearer_auth(token).json(&cuerpo);
        self.enviar(req).await
    }

    /// HU2-18/20/21/22: una consulta a Pagos por el Broker, con filtros (HU2-23).
    pub async fn consultar<T: DeserializeOwned>(
        &self,
        token: &str,
        query: &str,
        params: Map<String, Value>,
    ) -> Result<Consulta<T>, ErrorApi> {
        let cuerpo = json!({"query": query, "params": params});
        let req = self.http.post(self.url("/consultas")).bearer_auth(token).json(&cuerpo);
        self.enviar(req).await
    }

    /// Une la URL base con la ruta.
    fn url(&self, ruta: &str) -> String {
        format!("{}{}", self.base, ruta)
    }

    /// Envía la petición y lee el JSON, o traduce el error.
    async fn enviar<T: DeserializeOwned>(&self, req: RequestBuilder) -> Result<T, ErrorApi> {
        let resp = req.send().await.map_err(|e| self.error_de_red(&e))?;
        let estado = resp.status().as_u16();
        let cuerpo = resp.text().await.map_err(|e| self.error_de_red(&e))?;
        if !(200..300).contains(&estado) {
            return Err(error_de_respuesta(estado, &cuerpo));
        }
        serde_json::from_str(&cuerpo).map_err(|e| ErrorApi::Servidor {
            estado,
            mensaje: format!("Respuesta inesperada de Admin: {e}"),
        })
    }

    /// Error sin respuesta de Admin: timeout (RF-30) o no hay conexión.
    fn error_de_red(&self, e: &reqwest::Error) -> ErrorApi {
        if e.is_timeout() {
            ErrorApi::TardoDemasiado(format!(
                "La consulta tardó demasiado: Admin no respondió en {} s",
                TIMEOUT.as_secs()
            ))
        } else {
            ErrorApi::SinConexion(format!("No se pudo conectar con Admin en {}", self.base))
        }
    }
}

#[cfg(test)]
mod pruebas {
    use super::*;

    #[test]
    fn el_401_y_el_403_conservan_el_mensaje_de_admin() {
        let e = error_de_respuesta(401, r#"{"detail":"Correo o contraseña incorrectos"}"#);
        assert_eq!(e, ErrorApi::SinSesion("Correo o contraseña incorrectos".into()));
        let e = error_de_respuesta(403, r#"{"detail":"Solo los administradores pueden usar la CLI"}"#);
        assert_eq!(e.mensaje(), "Solo los administradores pueden usar la CLI");
    }

    #[test]
    fn el_504_es_tardo_demasiado() {
        let e = error_de_respuesta(
            504,
            r#"{"detail":"La consulta tardó demasiado: Pagos no respondió en 10 s"}"#,
        );
        assert!(matches!(e, ErrorApi::TardoDemasiado(_)));
    }

    #[test]
    fn el_422_nombra_el_campo_invalido() {
        // Respuesta real de Admin con un evento_id que no es UUID.
        let cuerpo = r#"{"detail":[{"type":"uuid_parsing","loc":["body","params","evento_id"],"msg":"Input should be a valid UUID"}]}"#;
        assert_eq!(
            error_de_respuesta(422, cuerpo),
            ErrorApi::Servidor {
                estado: 422,
                mensaje: "Dato inválido en «evento_id»".into()
            }
        );
    }

    #[test]
    fn un_cuerpo_que_no_es_json_igual_da_un_mensaje() {
        assert_eq!(
            error_de_respuesta(502, "<html>Bad Gateway</html>").mensaje(),
            "Admin respondió 502"
        );
    }

    #[tokio::test]
    async fn sin_admin_avisa_que_no_hay_conexion() {
        // Puerto 9 (discard): nadie escucha ahí.
        let cliente = ClienteAdmin::nuevo("http://127.0.0.1:9/");
        let e = cliente.carreras("x.y.z").await.unwrap_err();
        assert_eq!(
            e,
            ErrorApi::SinConexion("No se pudo conectar con Admin en http://127.0.0.1:9".into())
        );
    }
}
