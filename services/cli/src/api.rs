//! Cliente REST de Admin. Desde el escenario 2 (T3-03/T3-06) Admin solo se
//! alcanza a través del Gateway, por HTTPS, en `/api/admin`. Es la única puerta
//! de la CLI: nunca habla con Pagos, el Ingestor ni OpenF1 (Vista C&C).
//!
//! El certificado del Gateway siempre se verifica. Si es el autofirmado del
//! proyecto, se confía con `POLEBET_CA_CERT` (ruta de `polebet.crt`); nunca se
//! desactiva la verificación.
//!
//! Cada error se traduce a `ErrorApi` con un mensaje listo para mostrar.

use std::error::Error as _;
use std::path::Path;
use std::time::Duration;

use reqwest::{Certificate, Client, RequestBuilder};
use serde::de::DeserializeOwned;
use serde_json::{json, Map, Value};

use crate::modelos::{Carrera, Consulta, RespuestaCarreras, Sesion, SimulacionProgramada};

/// Admin corta a los 10 s con un 504 (RF-30). La CLI espera un poco más para
/// recibir ese 504 con su mensaje; si ni eso llega, avisa por su cuenta.
pub const TIMEOUT: Duration = Duration::from_secs(12);

/// Variable con la ruta de un certificado PEM en el que confiar además de las
/// raíces públicas (el autofirmado del Gateway: services/gateway/certs/polebet.crt).
pub const VAR_CA_CERT: &str = "POLEBET_CA_CERT";

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
    /// Crea el cliente para la URL de Admin (p. ej. https://localhost/api/admin).
    /// `raices_extra` se suman a las raíces públicas como certificados de confianza.
    pub fn nuevo(base: &str, raices_extra: Vec<Certificate>) -> Result<Self, String> {
        let mut builder = Client::builder()
            .timeout(TIMEOUT)
            .connect_timeout(Duration::from_secs(3));
        for cert in raices_extra {
            builder = builder.add_root_certificate(cert);
        }
        let http = builder
            .build()
            .map_err(|e| format!("No se pudo preparar el cliente HTTPS: {}", cadena_de_error(&e)))?;
        Ok(ClienteAdmin {
            http,
            base: base.trim_end_matches('/').to_string(),
        })
    }

    /// Como `nuevo`, pero confía además en el certificado de `POLEBET_CA_CERT` si está definida.
    pub fn desde_entorno(base: &str) -> Result<Self, String> {
        let raices = match std::env::var(VAR_CA_CERT) {
            Ok(ruta) if !ruta.trim().is_empty() => leer_certificados(Path::new(ruta.trim()))?,
            _ => Vec::new(),
        };
        Self::nuevo(base, raices)
    }

    /// true si la URL de Admin va sin TLS (http://): el token y la contraseña viajarían en claro.
    pub fn sin_cifrar(&self) -> bool {
        sin_cifrar(&self.base)
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
        } else if let Some(causa) = causa_de_certificado(e) {
            ErrorApi::SinConexion(format!(
                "El certificado de {} no es de confianza ({causa}). Si es el autofirmado del \
                 Gateway, define {VAR_CA_CERT} con la ruta de polebet.crt",
                self.base
            ))
        } else {
            ErrorApi::SinConexion(format!("No se pudo conectar con Admin en {}", self.base))
        }
    }
}

/// true si la URL va por HTTP plano (sin TLS).
pub fn sin_cifrar(url: &str) -> bool {
    url.trim().to_ascii_lowercase().starts_with("http://")
}

/// Lee un archivo PEM con uno o más certificados. Error claro si no existe,
/// no se puede leer o no trae ningún certificado (p. ej. si se pasó la llave).
pub fn leer_certificados(ruta: &Path) -> Result<Vec<Certificate>, String> {
    let nombre = ruta.display();
    let pem = std::fs::read(ruta).map_err(|e| format!("{VAR_CA_CERT}: no se pudo leer {nombre} ({e})"))?;
    let certs = Certificate::from_pem_bundle(&pem)
        .map_err(|_| format!("{VAR_CA_CERT}: {nombre} no es un certificado PEM válido"))?;
    if certs.is_empty() {
        return Err(format!(
            "{VAR_CA_CERT}: {nombre} no contiene ningún certificado (-----BEGIN CERTIFICATE-----)"
        ));
    }
    Ok(certs)
}

/// Une el mensaje del error con los de sus causas (reqwest esconde el detalle de TLS en ellas).
fn cadena_de_error(e: &(dyn std::error::Error + 'static)) -> String {
    let mut partes = vec![e.to_string()];
    let mut causa = e.source();
    while let Some(c) = causa {
        partes.push(c.to_string());
        causa = c.source();
    }
    partes.join(": ")
}

/// Si la conexión falló al verificar el certificado del servidor, devuelve la
/// causa que da rustls («invalid peer certificate: UnknownIssuer», «NotValidForName»...).
fn causa_de_certificado(e: &reqwest::Error) -> Option<String> {
    if !e.is_connect() {
        return None;
    }
    let mut causa = e.source();
    while let Some(c) = causa {
        let texto = c.to_string();
        if texto.to_ascii_lowercase().contains("certificate") {
            return Some(texto);
        }
        causa = c.source();
    }
    None
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
        let cliente = ClienteAdmin::nuevo("http://127.0.0.1:9/", Vec::new()).unwrap();
        let e = cliente.carreras("x.y.z").await.unwrap_err();
        assert_eq!(
            e,
            ErrorApi::SinConexion("No se pudo conectar con Admin en http://127.0.0.1:9".into())
        );
    }

    /// Certificado autofirmado de prueba (CN=prueba-cli), solo para leerlo.
    const PEM_PRUEBA: &str = "-----BEGIN CERTIFICATE-----
MIIBfzCCASWgAwIBAgIUF00P5RxszLr+Shtu7z2KMDB0KWowCgYIKoZIzj0EAwIw
FTETMBEGA1UEAwwKcHJ1ZWJhLWNsaTAeFw0yNjExMjAwMDEwNTBaFw0zNjExMTcw
MDEwNTBaMBUxEzARBgNVBAMMCnBydWViYS1jbGkwWTATBgcqhkjOPQIBBggqhkjO
PQMBBwNCAARFTILPRJshklWlYAy6g4XaYcgIikTrbVIoHrs7VWuwMm4lB6+L+pVY
OUfjHAoJBJ8h4kEU7weiMy3R40+JERV4o1MwUTAdBgNVHQ4EFgQU0vPwCYSLCU0F
RQeVKykfhqs/xU0wHwYDVR0jBBgwFoAU0vPwCYSLCU0FRQeVKykfhqs/xU0wDwYD
VR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiBUiAdtNT69Rtf02e5YGMIO
t1Dw18oGeFFJ/zvImiYDZQIhAKddatfTc3AhSUwYSlK3RhArkQ2LK+MR+qrpw80k
mf+a
-----END CERTIFICATE-----
";

    /// Escribe `contenido` en un archivo temporal propio de la prueba.
    fn archivo_temporal(nombre: &str, contenido: &str) -> std::path::PathBuf {
        let ruta = std::env::temp_dir().join(format!("polebet-cli-{}-{nombre}", std::process::id()));
        std::fs::write(&ruta, contenido).unwrap();
        ruta
    }

    #[test]
    fn lee_un_certificado_pem_valido() {
        let ruta = archivo_temporal("valido.crt", PEM_PRUEBA);
        let certs = leer_certificados(&ruta).unwrap();
        std::fs::remove_file(&ruta).ok();
        assert_eq!(certs.len(), 1);
        // Y el cliente arranca confiando en él.
        assert!(ClienteAdmin::nuevo("https://localhost/api/admin", certs).is_ok());
    }

    #[test]
    fn un_certificado_que_no_existe_da_un_error_claro() {
        let e = leer_certificados(Path::new("/no/existe/polebet.crt")).err().unwrap();
        assert!(
            e.starts_with("POLEBET_CA_CERT: no se pudo leer /no/existe/polebet.crt"),
            "{e}"
        );
    }

    #[test]
    fn un_archivo_sin_certificado_se_rechaza() {
        // Por ejemplo, pasar la llave privada en vez del certificado.
        let ruta = archivo_temporal(
            "llave.key",
            "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIA==\n-----END PRIVATE KEY-----\n",
        );
        let e = leer_certificados(&ruta).err().unwrap();
        std::fs::remove_file(&ruta).ok();
        assert!(e.contains("no contiene ningún certificado"), "{e}");
    }

    #[test]
    fn un_pem_corrupto_se_rechaza() {
        let ruta = archivo_temporal(
            "corrupto.crt",
            "-----BEGIN CERTIFICATE-----\n%%%no-es-base64%%%\n-----END CERTIFICATE-----\n",
        );
        let e = leer_certificados(&ruta).err().unwrap();
        std::fs::remove_file(&ruta).ok();
        assert!(e.contains("POLEBET_CA_CERT"), "{e}");
    }

    #[test]
    fn distingue_http_de_https() {
        assert!(sin_cifrar("http://localhost:8003"));
        assert!(sin_cifrar("HTTP://otra"));
        assert!(!sin_cifrar("https://localhost/api/admin"));
        let cliente = ClienteAdmin::nuevo("https://gateway/api/admin", Vec::new()).unwrap();
        assert!(!cliente.sin_cifrar());
    }
}
