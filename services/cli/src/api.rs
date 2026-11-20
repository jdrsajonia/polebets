//! Cliente REST de Admin (puerto 8003). Es la única puerta de la CLI: nunca
//! habla con Pagos, el Ingestor ni OpenF1 (Vista C&C).
//!
//! Habla HTTP o HTTPS según la URL (T3-06). Con HTTPS siempre verifica el
//! certificado: confía en las CA públicas y, si se le pasa, en la CA local del
//! Gateway (scripts/generar-certs.sh). Nunca apaga la verificación.
//!
//! Cada error se traduce a `ErrorApi` con un mensaje listo para mostrar.

use std::path::Path;
use std::time::Duration;

use reqwest::{Certificate, Client, RequestBuilder};
use rustls::CertificateError;
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

/// Lee la CA (PEM) en la que la CLI debe confiar, p. ej. services/gateway/certs/ca.crt.
pub fn leer_ca(ruta: &Path) -> Result<Vec<Certificate>, String> {
    let pem = std::fs::read(ruta).map_err(|e| format!("No se pudo leer la CA {}: {e}", ruta.display()))?;
    let certificados = Certificate::from_pem_bundle(&pem)
        .map_err(|_| format!("{} no es un certificado PEM válido", ruta.display()))?;
    if certificados.is_empty() {
        return Err(format!("{} no contiene ningún certificado PEM", ruta.display()));
    }
    Ok(certificados)
}

impl ClienteAdmin {
    /// Crea el cliente para la URL de Admin (p. ej. https://localhost/admin).
    /// Con `ca`, además de las CA públicas confía en esa (la local del Gateway).
    pub fn nuevo(base: &str, ca: Option<&Path>) -> Result<Self, String> {
        let mut builder = Client::builder()
            .timeout(TIMEOUT)
            .connect_timeout(Duration::from_secs(3));
        if let Some(ruta) = ca {
            for certificado in leer_ca(ruta)? {
                builder = builder.add_root_certificate(certificado);
            }
        }
        // Aquí rustls valida la CA: un PEM con un certificado roto falla al construir.
        let http = builder
            .build()
            .map_err(|e| format!("No se pudo preparar el cliente HTTPS: {}", causa_raiz(&e)))?;
        Ok(ClienteAdmin {
            http,
            base: base.trim_end_matches('/').to_string(),
        })
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

    /// Error sin respuesta de Admin: timeout (RF-30), TLS rechazado o no hay conexión.
    fn error_de_red(&self, e: &reqwest::Error) -> ErrorApi {
        if e.is_timeout() {
            ErrorApi::TardoDemasiado(format!(
                "La consulta tardó demasiado: Admin no respondió en {} s",
                TIMEOUT.as_secs()
            ))
        } else if let Some(tls) = error_tls(e) {
            ErrorApi::SinConexion(mensaje_tls(tls, &self.base))
        } else {
            ErrorApi::SinConexion(format!("No se pudo conectar con Admin en {}", self.base))
        }
    }
}

/// Busca el error de rustls dentro de la cadena de causas de reqwest. Viaja
/// envuelto en io::Error anidados (hyper → io::Error → io::Error → rustls), y el
/// `source()` de un io::Error se salta el error que guarda: por eso en cada
/// io::Error se sigue por `get_ref()`.
fn error_tls<'a>(e: &'a (dyn std::error::Error + 'static)) -> Option<&'a rustls::Error> {
    let mut actual = Some(e);
    while let Some(error) = actual {
        if let Some(tls) = error.downcast_ref::<rustls::Error>() {
            return Some(tls);
        }
        actual = match error.downcast_ref::<std::io::Error>() {
            Some(io) => io
                .get_ref()
                .map(|interno| interno as &(dyn std::error::Error + 'static)),
            None => error.source(),
        };
    }
    None
}

/// Explica por qué no se aceptó el HTTPS de Admin y qué hacer.
fn mensaje_tls(error: &rustls::Error, base: &str) -> String {
    match error {
        rustls::Error::InvalidCertificate(CertificateError::UnknownIssuer) => format!(
            "El certificado de {base} no es de una CA de confianza: pasa la CA del Gateway con --ca o POLEBET_CA_CERT"
        ),
        rustls::Error::InvalidCertificate(
            CertificateError::NotValidForName | CertificateError::NotValidForNameContext { .. },
        ) => {
            format!("El certificado de {base} no vale para ese nombre: usa uno de los que tiene (localhost o gateway)")
        }
        rustls::Error::InvalidCertificate(CertificateError::Expired | CertificateError::ExpiredContext { .. }) => {
            format!("El certificado de {base} está vencido: regenéralo con scripts/generar-certs.sh --forzar")
        }
        otro => format!("No se pudo establecer HTTPS con {base}: {otro}"),
    }
}

/// Última causa de un error, que es la que explica qué pasó.
fn causa_raiz(e: &(dyn std::error::Error + 'static)) -> String {
    let mut actual = e;
    while let Some(siguiente) = actual.source() {
        actual = siguiente;
    }
    actual.to_string()
}

#[cfg(test)]
mod pruebas {
    use super::*;

    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::path::PathBuf;
    use std::sync::Arc;

    use rustls::pki_types::pem::PemObject;
    use rustls::pki_types::{CertificateDer, PrivateKeyDer};

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
        let cliente = ClienteAdmin::nuevo("http://127.0.0.1:9/", None).unwrap();
        let e = cliente.carreras("x.y.z").await.unwrap_err();
        assert_eq!(
            e,
            ErrorApi::SinConexion("No se pudo conectar con Admin en http://127.0.0.1:9".into())
        );
    }

    // ---- T3-06: HTTPS ----

    /// Archivo de tests/certs: una CA y un certificado para localhost, solo para pruebas.
    fn cert_de_prueba(nombre: &str) -> PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/certs").join(nombre)
    }

    /// Levanta un servidor HTTPS de una sola conexión en 127.0.0.1, con el
    /// certificado de prueba, que responde a cualquier petición con `{"carreras":[]}`.
    /// Devuelve el puerto.
    fn servidor_https() -> u16 {
        let certificados = CertificateDer::pem_file_iter(cert_de_prueba("servidor.crt"))
            .unwrap()
            .collect::<Result<Vec<_>, _>>()
            .unwrap();
        let llave = PrivateKeyDer::from_pem_file(cert_de_prueba("servidor.key")).unwrap();
        let config = rustls::ServerConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_no_client_auth()
            .with_single_cert(certificados, llave)
            .unwrap();
        let config = Arc::new(config);
        let escucha = TcpListener::bind("127.0.0.1:0").unwrap();
        let puerto = escucha.local_addr().unwrap().port();
        std::thread::spawn(move || {
            let Ok((tcp, _)) = escucha.accept() else { return };
            let mut tls = rustls::StreamOwned::new(rustls::ServerConnection::new(config).unwrap(), tcp);
            // Lee hasta el fin de las cabeceras. Si el cliente rechazó el
            // certificado, el handshake falla aquí y no hay nada que responder.
            let mut leido = Vec::new();
            let mut trozo = [0u8; 1024];
            while !leido.windows(4).any(|w| w == b"\r\n\r\n") {
                match tls.read(&mut trozo) {
                    Ok(0) | Err(_) => return,
                    Ok(n) => leido.extend_from_slice(&trozo[..n]),
                }
            }
            let cuerpo = r#"{"carreras":[]}"#;
            let _ = write!(
                tls,
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{cuerpo}",
                cuerpo.len()
            );
            tls.conn.send_close_notify();
            let _ = tls.flush();
        });
        puerto
    }

    #[tokio::test]
    async fn con_la_ca_del_gateway_habla_https() {
        let base = format!("https://localhost:{}", servidor_https());
        let cliente = ClienteAdmin::nuevo(&base, Some(&cert_de_prueba("ca.crt"))).unwrap();
        assert_eq!(cliente.carreras("x.y.z").await, Ok(vec![]));
    }

    #[tokio::test]
    async fn sin_la_ca_rechaza_el_certificado_y_dice_que_hacer() {
        // Sin --ca la CLI solo confía en las CA públicas, y la del Gateway no lo es.
        let base = format!("https://localhost:{}", servidor_https());
        let cliente = ClienteAdmin::nuevo(&base, None).unwrap();
        assert_eq!(
            cliente.carreras("x.y.z").await.unwrap_err(),
            ErrorApi::SinConexion(format!(
                "El certificado de {base} no es de una CA de confianza: pasa la CA del Gateway con --ca o POLEBET_CA_CERT"
            ))
        );
    }

    #[tokio::test]
    async fn un_nombre_que_no_esta_en_el_certificado_se_rechaza() {
        // El certificado de prueba solo vale para localhost, no para 127.0.0.1.
        let base = format!("https://127.0.0.1:{}", servidor_https());
        let cliente = ClienteAdmin::nuevo(&base, Some(&cert_de_prueba("ca.crt"))).unwrap();
        assert_eq!(
            cliente.carreras("x.y.z").await.unwrap_err(),
            ErrorApi::SinConexion(format!(
                "El certificado de {base} no vale para ese nombre: usa uno de los que tiene (localhost o gateway)"
            ))
        );
    }

    #[test]
    fn una_ca_que_no_existe_no_deja_crear_el_cliente() {
        let error = ClienteAdmin::nuevo("https://localhost", Some(Path::new("no-existe/ca.crt")))
            .err()
            .unwrap();
        assert!(error.starts_with("No se pudo leer la CA no-existe/ca.crt"), "{error}");
    }

    #[test]
    fn una_llave_no_sirve_como_ca() {
        // Error fácil de cometer: pasar polebet.key en lugar de ca.crt.
        let ruta = cert_de_prueba("servidor.key");
        let error = ClienteAdmin::nuevo("https://localhost", Some(&ruta)).err().unwrap();
        assert_eq!(error, format!("{} no contiene ningún certificado PEM", ruta.display()));
    }
}
