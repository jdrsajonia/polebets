//! Prueba contra Admin de verdad (stack levantado). Se salta por defecto:
//!
//!   POLEBET_ADMIN_URL=http://localhost:8003 POLEBET_ADMIN_PASSWORD=... \
//!     cargo test --test en_vivo -- --ignored --nocapture
//!
//! Maneja la app con las mismas teclas que el administrador y dibuja cada
//! pantalla en una terminal falsa de 120×32 (se imprime con --nocapture).

use polebet_cli::api::ClienteAdmin;
use polebet_cli::app::{App, Envio, Pantalla, Respuesta};
use polebet_cli::{ejecutor, ui};
use ratatui::backend::TestBackend;
use ratatui::crossterm::event::{KeyCode, KeyEvent};
use ratatui::Terminal;

/// Variable de entorno o valor por defecto.
fn entorno(nombre: &str, defecto: &str) -> String {
    std::env::var(nombre).unwrap_or_else(|_| defecto.into())
}

/// Corre las peticiones contra Admin y las que salgan de sus respuestas.
async fn procesar(app: &mut App, cliente: &ClienteAdmin, mut envios: Vec<Envio>) {
    while let Some(envio) = envios.pop() {
        let resultado = ejecutor::resolver(cliente, envio.peticion).await;
        envios.extend(app.recibir(Respuesta {
            id: envio.id,
            resultado,
        }));
    }
}

/// Escribe un texto tecla por tecla.
fn escribir(app: &mut App, texto: &str) {
    for c in texto.chars() {
        app.tecla(KeyEvent::from(KeyCode::Char(c)));
    }
}

/// Texto de la pantalla dibujada.
fn pantalla(app: &App) -> String {
    let mut terminal = Terminal::new(TestBackend::new(120, 32)).unwrap();
    terminal.draw(|f| ui::dibujar(f, app)).unwrap();
    let b = terminal.backend().buffer();
    (0..b.area.height)
        .map(|y| (0..b.area.width).map(|x| b[(x, y)].symbol()).collect::<String>())
        .collect::<Vec<_>>()
        .join("\n")
}

/// Inicia sesión con correo y contraseña como lo haría el administrador.
async fn ingresar(app: &mut App, cliente: &ClienteAdmin, email: &str, password: &str) {
    escribir(app, email);
    app.tecla(KeyEvent::from(KeyCode::Tab));
    escribir(app, password);
    let envios = app.tecla(KeyEvent::from(KeyCode::Enter));
    procesar(app, cliente, envios).await;
}

#[tokio::test]
#[ignore = "necesita el stack levantado y POLEBET_ADMIN_PASSWORD"]
async fn el_admin_entra_y_recorre_los_paneles() {
    let url = entorno("POLEBET_ADMIN_URL", "http://localhost:8003");
    let password = std::env::var("POLEBET_ADMIN_PASSWORD").expect("falta POLEBET_ADMIN_PASSWORD");
    let cliente = ClienteAdmin::nuevo(&url);
    let mut app = App::nuevo(url);

    ingresar(
        &mut app,
        &cliente,
        &entorno("POLEBET_ADMIN_EMAIL", "admin@arqui.local"),
        &password,
    )
    .await;
    assert_eq!(
        app.pantalla,
        Pantalla::Principal,
        "error de login: {:?}",
        app.login.error
    );
    println!("{}\n", pantalla(&app));
    assert!(app.carreras.datos().is_some(), "carreras: {:?}", app.carreras);

    for (tecla, nombre) in [
        ('2', "resumen"),
        ('3', "rentabilidad"),
        ('4', "horarios"),
        ('5', "participacion"),
    ] {
        let envios = app.tecla(KeyEvent::from(KeyCode::Char(tecla)));
        procesar(&mut app, &cliente, envios).await;
        println!("{}\n", pantalla(&app));
        let listo = match tecla {
            '2' => app.resumen.datos().is_some(),
            '3' => app.rentabilidad.datos().is_some(),
            '4' => app.horarios.datos().is_some(),
            _ => app.participacion.datos().is_some(),
        };
        assert!(listo, "el panel {nombre} no cargó");
    }
}

#[tokio::test]
#[ignore = "necesita el stack levantado y POLEBET_JUGADOR_EMAIL / POLEBET_JUGADOR_PASSWORD"]
async fn un_jugador_no_entra() {
    let url = entorno("POLEBET_ADMIN_URL", "http://localhost:8003");
    let email = std::env::var("POLEBET_JUGADOR_EMAIL").expect("falta POLEBET_JUGADOR_EMAIL");
    let password = std::env::var("POLEBET_JUGADOR_PASSWORD").expect("falta POLEBET_JUGADOR_PASSWORD");
    let cliente = ClienteAdmin::nuevo(&url);
    let mut app = App::nuevo(url);

    ingresar(&mut app, &cliente, &email, &password).await;
    println!("{}\n", pantalla(&app));
    assert_eq!(app.pantalla, Pantalla::Login);
    assert_eq!(
        app.login.error.as_deref(),
        Some("Solo los administradores pueden usar la CLI")
    );
}
