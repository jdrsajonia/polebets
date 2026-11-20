//! CLI del administrador de PoleBet (HU2-17): terminal al estilo lazydocker.
//!
//!   polebet-cli [--url https://localhost/api/admin]
//!
//! La URL de Admin sale de `--url`, de POLEBET_ADMIN_URL o, si no, de
//! https://localhost/api/admin: Admin está detrás del Gateway (escenario 2) y
//! se habla con él por HTTPS. Si el certificado es el autofirmado del proyecto,
//! POLEBET_CA_CERT dice dónde está polebet.crt para confiar en él.
//!
//! El bucle dibuja, lee el teclado cada 100 ms y recoge las respuestas de Admin; las peticiones corren en tareas de tokio,
//! así una consulta lenta nunca congela la pantalla (RF-30).

use std::io;
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

Uso: polebet-cli [--url URL_DE_ADMIN]

  --url URL   dónde está Admin (por defecto POLEBET_ADMIN_URL o https://localhost/api/admin)
  -h, --help  muestra esta ayuda

Variables de entorno:
  POLEBET_ADMIN_URL  URL de Admin (la misma que --url)
  POLEBET_CA_CERT    ruta de un certificado PEM en el que confiar, p. ej. el
                     autofirmado del Gateway: services/gateway/certs/polebet.crt";

/// Arranca la terminal, corre la CLI y la deja como estaba al salir.
fn main() -> io::Result<()> {
    let url = match url_de_argumentos(std::env::args().skip(1).collect()) {
        Ok(url) => url,
        Err(texto) => {
            println!("{texto}");
            return Ok(());
        }
    };
    // Errores de configuración (certificado ilegible, etc.): mensaje y salida,
    // antes de tomar la terminal.
    let cliente = match ClienteAdmin::desde_entorno(&url) {
        Ok(c) => c,
        Err(texto) => {
            eprintln!("polebet-cli: {texto}");
            std::process::exit(2);
        }
    };
    if cliente.sin_cifrar() {
        eprintln!(
            "AVISO: {url} es HTTP plano; la contraseña y el token viajan SIN CIFRAR. \
             Úsalo solo para pruebas (lo normal es https://.../api/admin)."
        );
        std::thread::sleep(Duration::from_secs(2));
    }
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

/// Lee `--url` (o `-h`); si no viene, usa POLEBET_ADMIN_URL o el valor por defecto.
fn url_de_argumentos(args: Vec<String>) -> Result<String, String> {
    let mut args = args.into_iter();
    let mut url = std::env::var("POLEBET_ADMIN_URL").ok().filter(|u| !u.trim().is_empty());
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "-h" | "--help" => return Err(AYUDA.into()),
            "--url" => url = Some(args.next().ok_or("falta la URL después de --url")?),
            otro => return Err(format!("argumento desconocido: {otro}\n\n{AYUDA}")),
        }
    }
    Ok(url.unwrap_or_else(|| URL_POR_DEFECTO.into()))
}
