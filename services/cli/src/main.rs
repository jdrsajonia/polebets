//! CLI del administrador de PoleBet (HU2-17): terminal al estilo lazydocker.
//!
//!   polebet-cli [--url https://localhost/api/admin] [--ca services/gateway/certs/ca.crt]
//!
//! La URL de Admin sale de `--url`, de POLEBET_ADMIN_URL o, si no, de
//! https://localhost/api/admin: Admin está detrás del Gateway (escenario 2,
//! T3-03). Con HTTPS (T3-06), `--ca` o POLEBET_CA_CERT dicen en qué CA confiar
//! además de las públicas: la local del Gateway, que crea scripts/generar-certs.sh. El bucle dibuja, lee el teclado cada 100 ms y
//! recoge las respuestas de Admin; las peticiones corren en tareas de tokio,
//! así una consulta lenta nunca congela la pantalla (RF-30).

use std::io;
use std::path::PathBuf;
use std::sync::mpsc::{self, Sender};
use std::time::Duration;

use polebet_cli::api::ClienteAdmin;
use polebet_cli::app::{App, Envio, Respuesta};
use polebet_cli::{ejecutor, ui};
use ratatui::crossterm::event::{self, Event, KeyEventKind};
use ratatui::DefaultTerminal;
use tokio::runtime::Runtime;

const URL_POR_DEFECTO: &str = "https://localhost/api/admin";

const AYUDA: &str = "CLI del administrador de PoleBet (HU2-17)

Uso: polebet-cli [--url URL_DE_ADMIN] [--ca RUTA]

  --url URL   dónde está Admin (por defecto POLEBET_ADMIN_URL o https://localhost/api/admin)
  --ca RUTA   CA en la que confiar para el HTTPS del Gateway, además de las públicas
              (por defecto POLEBET_CA_CERT; la local es services/gateway/certs/ca.crt)
  -h, --help  muestra esta ayuda";

/// Lo que se le pasa a la CLI al arrancarla.
#[derive(Debug, PartialEq)]
struct Opciones {
    url: String,
    ca: Option<PathBuf>,
}

/// Arranca la terminal, corre la CLI y la deja como estaba al salir.
fn main() -> io::Result<()> {
    let entorno = |nombre: &str| std::env::var(nombre).ok();
    let opciones = match opciones_de_argumentos(std::env::args().skip(1).collect(), entorno) {
        Ok(opciones) => opciones,
        Err(texto) => {
            println!("{texto}");
            return Ok(());
        }
    };
    // Una CA que no se puede leer se avisa antes de abrir la interfaz.
    let cliente = match ClienteAdmin::nuevo(&opciones.url, opciones.ca.as_deref()) {
        Ok(cliente) => cliente,
        Err(texto) => {
            eprintln!("{texto}");
            std::process::exit(1);
        }
    };
    let runtime = Runtime::new()?;
    let mut app = App::nuevo(cliente.base().to_string());

    // ratatui::init deja un panic hook que restaura la terminal si algo falla.
    let mut terminal = ratatui::init();
    let resultado = correr(&mut terminal, &mut app, &runtime, &cliente);
    ratatui::restore();
    resultado
}

/// Bucle principal: dibujar, atender teclas y recoger respuestas hasta salir.
fn correr(terminal: &mut DefaultTerminal, app: &mut App, runtime: &Runtime, cliente: &ClienteAdmin) -> io::Result<()> {
    let (tx, rx) = mpsc::channel::<Respuesta>();
    while !app.salir {
        terminal.draw(|f| ui::dibujar(f, app))?;
        // El timeout también hace girar el indicador de «Consultando…».
        if event::poll(Duration::from_millis(100))? {
            if let Event::Key(k) = event::read()? {
                if k.kind == KeyEventKind::Press {
                    for envio in app.tecla(k) {
                        lanzar(runtime, cliente, &tx, envio);
                    }
                }
            }
        }
        while let Ok(respuesta) = rx.try_recv() {
            for envio in app.recibir(respuesta) {
                lanzar(runtime, cliente, &tx, envio);
            }
        }
    }
    Ok(())
}

/// Corre la petición en otra tarea y devuelve la respuesta por el canal.
fn lanzar(runtime: &Runtime, cliente: &ClienteAdmin, tx: &Sender<Respuesta>, envio: Envio) {
    let cliente = cliente.clone();
    let tx = tx.clone();
    runtime.spawn(async move {
        let resultado = ejecutor::resolver(&cliente, envio.peticion).await;
        // Si la CLI ya cerró, nadie recibe: no importa.
        let _ = tx.send(Respuesta {
            id: envio.id,
            resultado,
        });
    });
}

/// Lee `--url`, `--ca` (o `-h`). Lo que no venga sale de POLEBET_ADMIN_URL y
/// POLEBET_CA_CERT (vía `entorno`) o del valor por defecto.
fn opciones_de_argumentos(args: Vec<String>, entorno: impl Fn(&str) -> Option<String>) -> Result<Opciones, String> {
    // Una variable vacía cuenta como no puesta.
    let variable = |nombre: &str| entorno(nombre).filter(|v| !v.trim().is_empty());
    let mut url = variable("POLEBET_ADMIN_URL");
    let mut ca = variable("POLEBET_CA_CERT").map(PathBuf::from);
    let mut args = args.into_iter();
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "-h" | "--help" => return Err(AYUDA.into()),
            "--url" => url = Some(args.next().ok_or("falta la URL después de --url")?),
            "--ca" => ca = Some(args.next().ok_or("falta la ruta de la CA después de --ca")?.into()),
            otro => return Err(format!("argumento desconocido: {otro}\n\n{AYUDA}")),
        }
    }
    Ok(Opciones {
        url: url.unwrap_or_else(|| URL_POR_DEFECTO.into()),
        ca,
    })
}

#[cfg(test)]
mod pruebas {
    use super::*;

    /// Entorno falso con las variables dadas.
    fn con<'a>(variables: &'a [(&'a str, &'a str)]) -> impl Fn(&str) -> Option<String> + 'a {
        move |nombre| variables.iter().find(|(n, _)| *n == nombre).map(|(_, v)| v.to_string())
    }

    /// Convierte los argumentos a Vec<String>.
    fn args(lista: &[&str]) -> Vec<String> {
        lista.iter().map(|a| a.to_string()).collect()
    }

    #[test]
    fn sin_nada_usa_admin_por_el_gateway_local_y_ninguna_ca() {
        let opciones = opciones_de_argumentos(vec![], con(&[])).unwrap();
        assert_eq!(
            opciones,
            Opciones {
                url: "https://localhost/api/admin".into(),
                ca: None
            }
        );
    }

    #[test]
    fn la_ca_sale_de_la_variable_y_el_argumento_manda() {
        let entorno = con(&[
            ("POLEBET_CA_CERT", "/certs/ca.crt"),
            ("POLEBET_ADMIN_URL", "https://gateway/api/admin"),
        ]);
        let opciones = opciones_de_argumentos(vec![], &entorno).unwrap();
        assert_eq!(opciones.ca, Some(PathBuf::from("/certs/ca.crt")));
        assert_eq!(opciones.url, "https://gateway/api/admin");

        let opciones =
            opciones_de_argumentos(args(&["--ca", "otra.crt", "--url", "https://localhost"]), &entorno).unwrap();
        assert_eq!(opciones.ca, Some(PathBuf::from("otra.crt")));
        assert_eq!(opciones.url, "https://localhost");
    }

    #[test]
    fn una_variable_vacia_no_cuenta() {
        let opciones = opciones_de_argumentos(vec![], con(&[("POLEBET_CA_CERT", " ")])).unwrap();
        assert_eq!(opciones.ca, None);
    }

    #[test]
    fn falta_la_ruta_despues_de_ca() {
        let error = opciones_de_argumentos(args(&["--ca"]), con(&[])).unwrap_err();
        assert_eq!(error, "falta la ruta de la CA después de --ca");
    }
}
