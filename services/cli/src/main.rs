//! CLI del administrador de PoleBet (HU2-17): terminal al estilo lazydocker.
//!
//!   polebet-cli [--url http://localhost:8003]
//!
//! La URL de Admin sale de `--url`, de POLEBET_ADMIN_URL o, si no, de
//! http://localhost:8003. El bucle dibuja, lee el teclado cada 100 ms y
//! recoge las respuestas de Admin; las peticiones corren en tareas de tokio,
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

const URL_POR_DEFECTO: &str = "http://localhost:8003";

const AYUDA: &str = "CLI del administrador de PoleBet (HU2-17)

Uso: polebet-cli [--url URL_DE_ADMIN]

  --url URL   dónde está Admin (por defecto POLEBET_ADMIN_URL o http://localhost:8003)
  -h, --help  muestra esta ayuda";

/// Arranca la terminal, corre la CLI y la deja como estaba al salir.
fn main() -> io::Result<()> {
    let url = match url_de_argumentos(std::env::args().skip(1).collect()) {
        Ok(url) => url,
        Err(texto) => {
            println!("{texto}");
            return Ok(());
        }
    };
    let runtime = Runtime::new()?;
    let cliente = ClienteAdmin::nuevo(&url);
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
