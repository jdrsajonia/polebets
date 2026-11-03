//! Dibujo de la CLI con ratatui, al estilo lazydocker: menú de paneles y
//! filtros a la izquierda, el panel elegido a la derecha y los atajos abajo.
//! Solo lee el estado de `App`; nunca lo cambia.

use std::time::{Instant, SystemTime, UNIX_EPOCH};

use ratatui::layout::{Alignment, Constraint, Layout, Rect};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::{Line, Span, Text};
use ratatui::widgets::{Block, BorderType, Borders, Cell, Clear, Padding, Paragraph, Row, Table, TableState, Wrap};
use ratatui::Frame;
use serde_json::Value;

use crate::app::{App, CampoFiltro, CampoLogin, Estado, Filtros, Foco, Panel, Pantalla, Popup, MINUTOS_MAX};
use crate::formato::{con_signo, fecha_hora_colombia, miles};
use crate::modelos::{Carrera, Consulta, FilaPlantilla, Horarios, Participacion, Rentabilidad, ResumenFinanciero};

// Paleta del mockup de PoleBet.
const ROJO: Color = Color::Rgb(225, 6, 0);
const ROJO_CLARO: Color = Color::Rgb(255, 99, 88);
const VERDE: Color = Color::Rgb(34, 197, 94);
const AMBAR: Color = Color::Rgb(245, 158, 11);
const GRIS: Color = Color::Rgb(161, 161, 170);
const TENUE: Color = Color::Rgb(90, 90, 100);
const FONDO_SEL: Color = Color::Rgb(60, 18, 20);
const SPINNER: [&str; 10] = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

/// Dibuja la pantalla que toque.
pub fn dibujar(f: &mut Frame, app: &App) {
    match app.pantalla {
        Pantalla::Login => dibujar_login(f, app),
        Pantalla::Principal => dibujar_principal(f, app),
    }
}

// ----------------------------------------------------------------------
// Piezas comunes
// ----------------------------------------------------------------------

/// Cuadro del indicador giratorio según la hora (cambia cada 100 ms).
fn giro() -> &'static str {
    let ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_millis());
    SPINNER[(ms / 100) as usize % SPINNER.len()]
}

/// Rectángulo de `ancho`×`alto` centrado en `area` (recortado si no cabe).
fn centrado(area: Rect, ancho: u16, alto: u16) -> Rect {
    let ancho = ancho.min(area.width);
    let alto = alto.min(area.height);
    Rect {
        x: area.x + (area.width - ancho) / 2,
        y: area.y + (area.height - alto) / 2,
        width: ancho,
        height: alto,
    }
}

/// Marco redondeado; rojo si tiene el foco.
fn marco(titulo: &str, activo: bool) -> Block<'static> {
    Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(if activo { ROJO } else { TENUE }))
        .title(Line::from(format!(" {titulo} ")).bold())
}

/// Marca de la app: "PoleBet" con "Bet" en rojo.
fn marca() -> Vec<Span<'static>> {
    vec![
        Span::raw(" Pole").bold(),
        Span::styled("Bet", Style::new().fg(ROJO).bold()),
    ]
}

/// Línea de atajos: tecla en blanco, descripción en gris.
fn atajos(lista: &[(&str, &str)]) -> Line<'static> {
    let mut spans = vec![Span::raw(" ")];
    for (tecla, que) in lista {
        spans.push(Span::raw(tecla.to_string()).bold());
        spans.push(Span::styled(format!(" {que}   "), Style::new().fg(GRIS)));
    }
    Line::from(spans)
}

/// Celda de número alineada a la derecha.
fn num(texto: String) -> Cell<'static> {
    Cell::from(Text::from(texto).alignment(Alignment::Right))
}

/// Celda de ganancia: verde si gana la casa, rojo si pierde.
fn ganancia(n: i64) -> Cell<'static> {
    let color = match n {
        n if n > 0 => VERDE,
        n if n < 0 => ROJO_CLARO,
        _ => GRIS,
    };
    Cell::from(Text::from(con_signo(n)).alignment(Alignment::Right)).style(Style::new().fg(color))
}

/// Encabezado de tabla en gris.
fn encabezado(titulos: &[&str], derecha_desde: usize) -> Row<'static> {
    Row::new(titulos.iter().enumerate().map(|(i, t)| {
        let texto = Text::from(t.to_string());
        Cell::from(if i >= derecha_desde {
            texto.alignment(Alignment::Right)
        } else {
            texto
        })
    }))
    .style(Style::new().fg(GRIS).bold())
}

/// Estilo de la fila seleccionada (más fuerte si el foco está en el contenido).
fn estilo_sel(app: &App) -> Style {
    if app.foco == Foco::Contenido {
        Style::new().bg(FONDO_SEL).bold()
    } else {
        Style::new()
    }
}

/// Muestra «Consultando…», el error o la invitación a consultar; devuelve los
/// datos si ya llegaron, para que el panel los dibuje.
fn dibujar_estado<'a, T>(f: &mut Frame, area: Rect, estado: &'a Estado<T>) -> Option<&'a T> {
    let lineas = match estado {
        Estado::Listo(datos) => return Some(datos),
        Estado::SinCargar => vec![Line::styled("Pulsa r para consultar.", Style::new().fg(GRIS))],
        Estado::Consultando { desde, .. } => vec![
            Line::styled(
                format!("{} Consultando… {} s", giro(), segundos(*desde)),
                Style::new().fg(AMBAR).bold(),
            ),
            Line::styled(
                "Puedes seguir navegando mientras llega la respuesta.",
                Style::new().fg(GRIS),
            ),
        ],
        Estado::Fallo(mensaje) => vec![
            Line::styled(format!("✗ {mensaje}"), Style::new().fg(ROJO_CLARO).bold()),
            Line::styled("Pulsa r para reintentar.", Style::new().fg(GRIS)),
        ],
    };
    f.render_widget(Paragraph::new(lineas).wrap(Wrap { trim: true }), area);
    None
}

/// Segundos desde `desde`.
fn segundos(desde: Instant) -> u64 {
    desde.elapsed().as_secs()
}

// ----------------------------------------------------------------------
// Login (RF-21)
// ----------------------------------------------------------------------

/// Cuadro de inicio de sesión centrado.
fn dibujar_login(f: &mut Frame, app: &App) {
    let area = centrado(f.area(), 64, 14);
    let l = &app.login;
    let campo = |etiqueta: &str, valor: String, activo: bool| {
        Line::from(vec![
            Span::styled(if activo { "▸ " } else { "  " }, Style::new().fg(ROJO)),
            Span::styled(
                format!("{etiqueta:<12}"),
                if activo {
                    Style::new().bold()
                } else {
                    Style::new().fg(GRIS)
                },
            ),
            Span::raw(valor),
            Span::styled(if activo { "▏" } else { "" }, Style::new().fg(ROJO)),
        ])
    };
    let estado = match (&l.pendiente, &l.error) {
        (Some(_), _) => Line::styled(format!("{} Ingresando…", giro()), Style::new().fg(AMBAR)),
        (None, Some(e)) => Line::styled(format!("✗ {e}"), Style::new().fg(ROJO_CLARO).bold()),
        (None, None) => Line::raw(""),
    };
    let lineas = vec![
        Line::styled("Inicia sesión con una cuenta de administrador.", Style::new().fg(GRIS)),
        Line::raw(""),
        campo("Correo", l.email.clone(), l.campo == CampoLogin::Correo),
        campo(
            "Contraseña",
            "•".repeat(l.password.chars().count()),
            l.campo == CampoLogin::Clave,
        ),
        Line::raw(""),
        estado,
        Line::raw(""),
        atajos(&[("Enter", "ingresar"), ("Tab", "cambiar campo"), ("Esc", "salir")]),
        Line::styled(format!(" Admin: {}", app.url), Style::new().fg(TENUE)),
    ];
    let mut titulo = marca();
    titulo.push(Span::raw(" · Administrador "));
    let bloque = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(ROJO))
        .title(Line::from(titulo))
        .padding(Padding::new(2, 2, 1, 0));
    f.render_widget(Clear, area);
    f.render_widget(Paragraph::new(lineas).block(bloque).wrap(Wrap { trim: false }), area);
}

// ----------------------------------------------------------------------
// Pantalla principal
// ----------------------------------------------------------------------

/// Cabecera, menú, filtros, panel, pie y popup.
fn dibujar_principal(f: &mut Frame, app: &App) {
    let [cab, cuerpo, pie] =
        Layout::vertical([Constraint::Length(1), Constraint::Min(0), Constraint::Length(1)]).areas(f.area());
    dibujar_cabecera(f, cab, app);

    let [lateral, contenido] = Layout::horizontal([Constraint::Length(30), Constraint::Min(0)]).areas(cuerpo);
    let [menu, filtros] =
        Layout::vertical([Constraint::Length(Panel::TODOS.len() as u16 + 2), Constraint::Min(0)]).areas(lateral);
    dibujar_menu(f, menu, app);
    dibujar_filtros(f, filtros, &app.filtros);

    let bloque = marco(app.panel.titulo(), app.foco == Foco::Contenido).padding(Padding::horizontal(1));
    let interior = bloque.inner(contenido);
    f.render_widget(bloque, contenido);
    match app.panel {
        Panel::Simulacion => panel_simulacion(f, interior, app),
        Panel::Resumen => panel_resumen(f, interior, app),
        Panel::Rentabilidad => panel_rentabilidad(f, interior, app),
        Panel::Horarios => panel_horarios(f, interior, app),
        Panel::Participacion => panel_participacion(f, interior, app),
    }

    dibujar_pie(f, pie, app);
    match &app.popup {
        Some(Popup::Programar {
            carrera,
            minutos,
            error,
            pendiente,
            ..
        }) => popup_programar(f, carrera, minutos, error.as_deref(), pendiente.is_some()),
        Some(Popup::Filtros { borrador, campo }) => popup_filtros(f, borrador, *campo),
        None => {}
    }
}

/// Barra superior: marca a la izquierda, administrador y Admin a la derecha.
fn dibujar_cabecera(f: &mut Frame, area: Rect, app: &App) {
    let mut izquierda = marca();
    izquierda.push(Span::styled("  CLI del administrador", Style::new().fg(GRIS)));
    f.render_widget(Paragraph::new(Line::from(izquierda)), area);
    if let Some(s) = &app.sesion {
        let derecha = format!("{} · {} · {} ", s.usuario.nombre, s.usuario.email, app.url);
        f.render_widget(
            Paragraph::new(Line::styled(derecha, Style::new().fg(GRIS))).alignment(Alignment::Right),
            area,
        );
    }
}

/// Lista de paneles con un indicador de su estado.
fn dibujar_menu(f: &mut Frame, area: Rect, app: &App) {
    let lineas: Vec<Line> = Panel::TODOS
        .iter()
        .enumerate()
        .map(|(i, p)| {
            let sel = *p == app.panel;
            let indicador = match fase(app, *p) {
                Fase::Consultando => Span::styled(format!(" {}", giro()), Style::new().fg(AMBAR)),
                Fase::Fallo => Span::styled(" !", Style::new().fg(ROJO_CLARO).bold()),
                _ => Span::raw(""),
            };
            let estilo = if sel {
                Style::new().bold().bg(FONDO_SEL)
            } else {
                Style::new().fg(GRIS)
            };
            Line::from(vec![
                Span::styled(if sel { "▸ " } else { "  " }, Style::new().fg(ROJO)),
                Span::styled(format!("{} {}", i + 1, p.titulo()), estilo),
                indicador,
            ])
        })
        .collect();
    f.render_widget(
        Paragraph::new(lineas).block(marco("Paneles", app.foco == Foco::Menu)),
        area,
    );
}

/// Fase de los datos de un panel, para el indicador del menú.
enum Fase {
    SinCargar,
    Consultando,
    Listo,
    Fallo,
}

/// Fase del panel `p`.
fn fase(app: &App, p: Panel) -> Fase {
    fn de<T>(e: &Estado<T>) -> Fase {
        match e {
            Estado::SinCargar => Fase::SinCargar,
            Estado::Consultando { .. } => Fase::Consultando,
            Estado::Listo(_) => Fase::Listo,
            Estado::Fallo(_) => Fase::Fallo,
        }
    }
    match p {
        Panel::Simulacion => de(&app.carreras),
        Panel::Resumen => de(&app.resumen),
        Panel::Rentabilidad => de(&app.rentabilidad),
        Panel::Horarios => de(&app.horarios),
        Panel::Participacion => de(&app.participacion),
    }
}

/// Recuadro con los filtros que tiene puestos el administrador (HU2-23).
fn dibujar_filtros(f: &mut Frame, area: Rect, filtros: &Filtros) {
    let activos = filtros.activos();
    let mut lineas: Vec<Line> = if activos.is_empty() {
        vec![Line::styled("Sin filtros", Style::new().fg(GRIS))]
    } else {
        activos
            .into_iter()
            .map(|(etiqueta, valor)| {
                Line::from(vec![
                    Span::styled(format!("{etiqueta}: "), Style::new().fg(GRIS)),
                    Span::raw(valor),
                ])
            })
            .collect()
    };
    lineas.push(Line::raw(""));
    lineas.push(Line::styled("f editar · c limpiar", Style::new().fg(TENUE)));
    f.render_widget(
        Paragraph::new(lineas)
            .wrap(Wrap { trim: false })
            .block(marco("Filtros", false)),
        area,
    );
}

/// Pie: el aviso de la última acción o los atajos del contexto.
fn dibujar_pie(f: &mut Frame, area: Rect, app: &App) {
    let linea = match (&app.aviso, app.foco, app.panel) {
        (Some(aviso), _, _) => {
            let (marca, color) = if aviso.ok { ("✓", VERDE) } else { ("✗", ROJO_CLARO) };
            Line::styled(format!(" {marca} {}", aviso.texto), Style::new().fg(color).bold())
        }
        (None, Foco::Menu, _) => atajos(&[
            ("↑↓", "panel"),
            ("→", "entrar"),
            ("1-5", "ir a"),
            ("r", "recargar"),
            ("f", "filtros"),
            ("c", "limpiar"),
            ("s", "cerrar sesión"),
            ("q", "salir"),
        ]),
        (None, Foco::Contenido, Panel::Simulacion) => atajos(&[
            ("↑↓", "carrera"),
            ("Enter", "programar"),
            ("←", "paneles"),
            ("r", "recargar"),
            ("q", "salir"),
        ]),
        (None, Foco::Contenido, _) => atajos(&[
            ("↑↓", "filas"),
            ("←", "paneles"),
            ("r", "recargar"),
            ("f", "filtros"),
            ("q", "salir"),
        ]),
    };
    f.render_widget(Paragraph::new(linea), area);
}

// ----------------------------------------------------------------------
// Paneles
// ----------------------------------------------------------------------

/// HU2-19: carreras descargadas; Enter programa la seleccionada.
fn panel_simulacion(f: &mut Frame, area: Rect, app: &App) {
    let [tabla, nota] = Layout::vertical([Constraint::Min(0), Constraint::Length(3)]).areas(area);
    let Some(carreras) = dibujar_estado(f, tabla, &app.carreras) else {
        return;
    };
    if carreras.is_empty() {
        let texto = vec![
            Line::styled("No hay carreras descargadas.", Style::new().bold()),
            Line::styled("Descarga una (una sola vez) con:", Style::new().fg(GRIS)),
            Line::raw("  docker exec -it arqui_ingestor /app/ingestor descargar <session_key>"),
        ];
        f.render_widget(Paragraph::new(texto).wrap(Wrap { trim: false }), tabla);
        return;
    }
    let filas = carreras.iter().map(|c: &Carrera| {
        Row::new(vec![
            Cell::from(c.carrera_id.clone()),
            Cell::from(c.nombre.clone()),
            Cell::from(c.circuito.clone()),
            num(c.temporada.map_or("—".into(), |t| t.to_string())),
            num(c.total_vueltas.map_or("—".into(), |v| v.to_string())),
            num(c.pilotos.to_string()),
            Cell::from(c.estado.clone()),
        ])
    });
    let t = Table::new(
        filas,
        [
            Constraint::Length(8),
            Constraint::Min(20),
            Constraint::Length(14),
            Constraint::Length(5),
            Constraint::Length(8),
            Constraint::Length(8),
            Constraint::Length(11),
        ],
    )
    .header(encabezado(
        &["ID", "Carrera", "Circuito", "Año", "Vueltas", "Pilotos", "Estado"],
        3,
    ))
    .row_highlight_style(estilo_sel(app))
    .highlight_symbol(if app.foco == Foco::Contenido { "▸ " } else { "  " });
    f.render_stateful_widget(
        t,
        tabla,
        &mut TableState::default().with_selected(Some(app.carrera_sel)),
    );
    let ayuda = "Enter (o p) programa la simulación de la carrera seleccionada. Solo puede haber una \
                 programada o en curso a la vez; la lista la da el Ingestor (MongoDB), nunca OpenF1.";
    f.render_widget(
        Paragraph::new(Line::styled(ayuda, Style::new().fg(GRIS))).wrap(Wrap { trim: true }),
        nota,
    );
}

/// Etiqueta legible de un filtro según el nombre que usa Admin.
fn etiqueta_filtro(clave: &str) -> &str {
    match clave {
        "evento_id" => "Evento",
        "fecha_inicio" => "Desde",
        "fecha_fin" => "Hasta",
        "tipo_apuesta" => "Tipo",
        "modo" => "Modo",
        "plantilla" => "Plantilla",
        "top_n" => "Top N",
        otra => otra,
    }
}

/// Dos líneas con los filtros que aplicó Admin y los que no aplican (HU2-23).
/// Antes de la respuesta muestra los pedidos.
fn linea_filtros<T>(f: &mut Frame, area: Rect, app: &App, consulta: Option<&Consulta<T>>) {
    let (aplicados, no_aplicados): (Vec<String>, Vec<String>) = match consulta {
        Some(c) => (
            c.filtros_aplicados
                .iter()
                .map(|(k, v)| {
                    let v = match v {
                        Value::String(s) => s.clone(),
                        otro => otro.to_string(),
                    };
                    format!("{}={v}", etiqueta_filtro(k))
                })
                .collect(),
            c.filtros_no_aplicados
                .iter()
                .map(|k| etiqueta_filtro(k).to_string())
                .collect(),
        ),
        None => (
            app.filtros
                .activos()
                .into_iter()
                .map(|(k, v)| format!("{k}={v}"))
                .collect(),
            vec![],
        ),
    };
    let mut lineas = vec![Line::from(vec![
        Span::styled("Filtros activos: ", Style::new().fg(GRIS)),
        if aplicados.is_empty() {
            Span::styled("ninguno", Style::new().fg(GRIS))
        } else {
            Span::styled(aplicados.join(" · "), Style::new().fg(AMBAR))
        },
    ])];
    if !no_aplicados.is_empty() {
        lineas.push(Line::styled(
            format!("No aplican a esta consulta: {}", no_aplicados.join(", ")),
            Style::new().fg(TENUE),
        ));
    }
    f.render_widget(Paragraph::new(lineas), area);
}

/// HU2-20: tokens que entran y salen, y la ganancia neta.
fn panel_resumen(f: &mut Frame, area: Rect, app: &App) {
    let [cab, cuerpo] = Layout::vertical([Constraint::Length(2), Constraint::Min(0)]).areas(area);
    linea_filtros(f, cab, app, app.resumen.datos());
    let Some(c) = dibujar_estado(f, cuerpo, &app.resumen) else {
        return;
    };
    let r: &ResumenFinanciero = &c.data;
    let fila = |etiqueta: &str, valor: String| Row::new(vec![Cell::from(etiqueta.to_string()), num(valor)]);
    let mut filas = vec![
        fila(
            "Tokens comprados",
            format!("{} ({} COP)", miles(r.tokens_comprados), miles(r.cop_comprados)),
        ),
        fila("Apostado en individual", miles(r.apostado_individual)),
        fila("Pagado en individual", miles(r.pagado_individual)),
        fila("Inscripciones cobradas", miles(r.inscripciones_cobradas)),
        fila("Inscripciones devueltas", miles(r.inscripciones_devueltas)),
        fila("Premios del torneo pagados", miles(r.premios_pagados)),
        fila("Botín en juego", miles(r.botin_en_juego)),
        fila("Apostado sin resolver", miles(r.apostado_pendiente)),
        Row::new(vec![Cell::from("─".repeat(30)), Cell::from("─".repeat(24))]).style(Style::new().fg(TENUE)),
    ];
    filas.push(Row::new(vec![
        Cell::from("Ganancia neta").bold(),
        ganancia(r.ganancia_neta).bold(),
    ]));
    let t = Table::new(filas, [Constraint::Length(30), Constraint::Length(24)])
        .header(encabezado(&["Concepto", "Tokens"], 1));
    f.render_widget(t, cuerpo);
}

/// Fila de la tabla de rentabilidad.
fn fila_plantilla(p: &FilaPlantilla) -> Row<'static> {
    Row::new(vec![
        Cell::from(p.kind.clone()),
        Cell::from(p.template.clone()),
        num(miles(p.apuestas)),
        num(format!("{:.1} %", p.acierto_pct)),
        num(miles(p.apostado)),
        num(miles(p.pagado)),
        ganancia(p.ganancia_neta),
        num(format!("×{:.2}", p.multiplicador_promedio)),
        num(miles(p.mercados_generados)),
    ])
}

/// Ranking corto (más o menos ganancia) con título.
fn ranking(titulo: &str, filas: &[FilaPlantilla]) -> Table<'static> {
    let rows = filas.iter().map(|p| {
        Row::new(vec![
            Cell::from(format!("{} ({})", p.template, p.kind)),
            ganancia(p.ganancia_neta),
        ])
    });
    Table::new(rows, [Constraint::Min(20), Constraint::Length(12)]).block(
        Block::new()
            .borders(Borders::TOP)
            .border_style(Style::new().fg(TENUE))
            .title(titulo.to_string()),
    )
}

/// HU2-21: rentabilidad por tipo y plantilla, y el ranking de plantillas.
fn panel_rentabilidad(f: &mut Frame, area: Rect, app: &App) {
    let datos = app.rentabilidad.datos();
    // El ranking solo aparece si trae plantillas (sin apuestas viene vacío).
    let alto_tops = datos.map_or(0, |c| match c.data.top_ganancia.len().max(c.data.peor_ganancia.len()) {
        0 => 0,
        n => n as u16 + 2,
    });
    let alto_tabla = datos.map_or(0, |c| c.data.filas.len() as u16 + 2);
    let [cab, totales, tabla, tops, _] = Layout::vertical([
        Constraint::Length(2),
        Constraint::Length(2),
        Constraint::Max(alto_tabla.max(3)),
        Constraint::Length(alto_tops),
        Constraint::Min(0),
    ])
    .areas(area);
    linea_filtros(f, cab, app, datos);
    let Some(c) = dibujar_estado(f, totales.union(tabla), &app.rentabilidad) else {
        return;
    };
    let r: &Rentabilidad = &c.data;
    let resumen = Line::from(vec![
        Span::styled("Apuestas ", Style::new().fg(GRIS)),
        Span::raw(miles(r.total_apuestas)),
        Span::styled(" · Apostado ", Style::new().fg(GRIS)),
        Span::raw(miles(r.total_apostado)),
        Span::styled(" · Pagado ", Style::new().fg(GRIS)),
        Span::raw(miles(r.total_pagado)),
        Span::styled(" · Ganancia neta ", Style::new().fg(GRIS)),
        Span::styled(
            con_signo(r.ganancia_neta),
            Style::new()
                .fg(if r.ganancia_neta < 0 { ROJO_CLARO } else { VERDE })
                .bold(),
        ),
    ]);
    f.render_widget(Paragraph::new(resumen), totales);
    if r.filas.is_empty() {
        f.render_widget(
            Paragraph::new(Line::styled("Sin mercados en el período.", Style::new().fg(GRIS))),
            tabla,
        );
        return;
    }
    let t = Table::new(
        r.filas.iter().map(fila_plantilla),
        [
            Constraint::Length(8),
            Constraint::Min(17),
            Constraint::Length(8),
            Constraint::Length(8),
            Constraint::Length(8),
            Constraint::Length(7),
            Constraint::Length(8),
            Constraint::Length(6),
            Constraint::Length(8),
        ],
    )
    .header(encabezado(
        &[
            "Tipo",
            "Plantilla",
            "Apuestas",
            "Acierto",
            "Apostado",
            "Pagado",
            "Ganancia",
            "Mult.",
            "Mercados",
        ],
        2,
    ))
    .row_highlight_style(estilo_sel(app));
    let seleccion = (app.foco == Foco::Contenido).then_some(app.fila);
    f.render_stateful_widget(t, tabla, &mut TableState::default().with_selected(seleccion));
    let [mas, menos] = Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)]).areas(tops);
    f.render_widget(ranking(" Más ganancia ", &r.top_ganancia), mas);
    f.render_widget(ranking(" Menos ganancia ", &r.peor_ganancia), menos);
}

/// HU2-22: volumen y ganancia por hora del día y por vuelta.
fn panel_horarios(f: &mut Frame, area: Rect, app: &App) {
    let [cab, cuerpo] = Layout::vertical([Constraint::Length(2), Constraint::Min(0)]).areas(area);
    linea_filtros(f, cab, app, app.horarios.datos());
    let Some(c) = dibujar_estado(f, cuerpo, &app.horarios) else {
        return;
    };
    let h: &Horarios = &c.data;
    if h.por_hora.is_empty() && h.por_vuelta.is_empty() {
        let texto = Line::styled("Sin apuestas individuales en el período.", Style::new().fg(GRIS));
        f.render_widget(Paragraph::new(texto), cuerpo);
        return;
    }
    let [izq, der] = Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)]).areas(cuerpo);
    let anchos = [Constraint::Length(10), Constraint::Length(12), Constraint::Length(12)];
    let marco_tabla = |t: &str| {
        Block::new()
            .borders(Borders::TOP)
            .border_style(Style::new().fg(TENUE))
            .title(t.to_string())
    };
    let por_hora = Table::new(
        h.por_hora.iter().map(|x| {
            Row::new(vec![
                Cell::from(format!("{:02}:00", x.hora)),
                num(miles(x.volumen)),
                ganancia(x.ganancia),
            ])
        }),
        anchos,
    )
    .header(encabezado(&["Hora", "Volumen", "Ganancia"], 1))
    .block(marco_tabla(" Por hora del día (Colombia) "));
    let por_vuelta = Table::new(
        h.por_vuelta.iter().map(|x| {
            let vuelta = x.vuelta.map_or("sin vuelta".into(), |v| format!("Vuelta {v}"));
            Row::new(vec![Cell::from(vuelta), num(miles(x.volumen)), ganancia(x.ganancia)])
        }),
        anchos,
    )
    .header(encabezado(&["Vuelta", "Volumen", "Ganancia"], 1))
    .block(marco_tabla(" Por vuelta de carrera "));
    // ↑/↓ desplaza las dos tablas a la vez.
    f.render_stateful_widget(por_hora, izq, &mut TableState::default().with_offset(app.fila));
    f.render_stateful_widget(por_vuelta, der, &mut TableState::default().with_offset(app.fila));
}

/// HU2-22: participación por modo en cada evento.
fn panel_participacion(f: &mut Frame, area: Rect, app: &App) {
    let [cab, cuerpo, pie] =
        Layout::vertical([Constraint::Length(2), Constraint::Min(0), Constraint::Length(1)]).areas(area);
    linea_filtros(f, cab, app, app.participacion.datos());
    let Some(c) = dibujar_estado(f, cuerpo, &app.participacion) else {
        return;
    };
    let p: &Participacion = &c.data;
    if p.eventos.is_empty() {
        f.render_widget(
            Paragraph::new(Line::styled("Sin eventos en el período.", Style::new().fg(GRIS))),
            cuerpo,
        );
        return;
    }
    let filas = p.eventos.iter().map(|e| {
        let largada = e
            .starts_at
            .as_deref()
            .and_then(fecha_hora_colombia)
            .unwrap_or_else(|| "—".into());
        Row::new(vec![
            Cell::from(e.race_key.clone()),
            Cell::from(largada),
            Cell::from(e.tournament_status.clone()),
            num(miles(e.inscritos)),
            num(miles(e.solo_individuales)),
            num(miles(e.botin)),
            ganancia(e.ganancia_torneo),
            ganancia(e.ganancia_individual),
            num(miles(e.apostado_pendiente)),
        ])
    });
    let t = Table::new(
        filas,
        [
            Constraint::Length(7),
            Constraint::Length(11),
            Constraint::Length(9),
            Constraint::Length(9),
            Constraint::Length(9),
            Constraint::Length(6),
            Constraint::Length(9),
            Constraint::Length(9),
            Constraint::Length(9),
        ],
    )
    .header(encabezado(
        &[
            "Carrera",
            "Largada",
            "Torneo",
            "Inscritos",
            "Solo ind.",
            "Botín",
            "G. torneo",
            "G. indiv.",
            "Pendiente",
        ],
        3,
    ))
    .row_highlight_style(estilo_sel(app));
    f.render_stateful_widget(t, cuerpo, &mut TableState::default().with_selected(Some(app.fila)));
    // El id del evento sirve para el filtro «Evento» (HU2-23).
    if let Some(e) = p.eventos.get(app.fila) {
        let linea = Line::from(vec![
            Span::styled("Evento seleccionado: ", Style::new().fg(GRIS)),
            Span::raw(e.event_id.clone()),
            Span::styled("  (úsalo en f → Evento)", Style::new().fg(TENUE)),
        ]);
        f.render_widget(Paragraph::new(linea), pie);
    }
}

// ----------------------------------------------------------------------
// Popups
// ----------------------------------------------------------------------

/// HU2-19: confirmar la simulación con sus minutos de anticipación.
fn popup_programar(f: &mut Frame, carrera: &Carrera, minutos: &str, error: Option<&str>, pendiente: bool) {
    let area = centrado(f.area(), 70, 15);
    let detalle = format!(
        "{} vueltas · {} pilotos",
        carrera.total_vueltas.map_or("—".into(), |v| v.to_string()),
        carrera.pilotos
    );
    let estado = match (pendiente, error) {
        (true, _) => Line::styled(format!("{} Programando…", giro()), Style::new().fg(AMBAR)),
        (false, Some(e)) => Line::styled(format!("✗ {e}"), Style::new().fg(ROJO_CLARO).bold()),
        (false, None) => Line::raw(""),
    };
    let lineas = vec![
        Line::from(vec![
            Span::styled("Carrera   ", Style::new().fg(GRIS)),
            Span::raw(carrera.nombre.clone()).bold(),
            Span::styled(format!("  ({})", carrera.carrera_id), Style::new().fg(GRIS)),
        ]),
        Line::from(vec![
            Span::styled("          ", Style::new()),
            Span::styled(detalle, Style::new().fg(GRIS)),
        ]),
        Line::raw(""),
        Line::from(vec![
            Span::raw("Minutos de anticipación  "),
            Span::styled(format!("[ {minutos:>3}▏]"), Style::new().fg(AMBAR).bold()),
        ]),
        Line::styled(
            format!("↑/↓ ±1 · PgUp/PgDn ±10 · o escribe el número (1 a {MINUTOS_MAX})"),
            Style::new().fg(GRIS),
        ),
        Line::raw(""),
        Line::styled(
            "Hasta la largada quedan abiertas las inscripciones al torneo y las apuestas estáticas.",
            Style::new().fg(GRIS),
        ),
        Line::raw(""),
        estado,
        Line::raw(""),
        atajos(&[("Enter", "confirmar"), ("Esc", "cancelar")]),
    ];
    let bloque = marco("Programar simulación", true).padding(Padding::new(2, 2, 1, 0));
    f.render_widget(Clear, area);
    f.render_widget(Paragraph::new(lineas).block(bloque).wrap(Wrap { trim: true }), area);
}

/// HU2-23: formulario de filtros.
fn popup_filtros(f: &mut Frame, borrador: &Filtros, campo: usize) {
    let area = centrado(f.area(), 74, 17);
    let mut lineas: Vec<Line> = CampoFiltro::TODOS
        .iter()
        .enumerate()
        .map(|(i, c)| {
            let sel = i == campo;
            let valor = borrador.valor(*c);
            let mostrado = match (valor.is_empty(), c.ayuda().starts_with('←'), sel) {
                (true, true, true) => Span::styled("‹ todos ›", Style::new().fg(AMBAR)),
                (true, _, _) => Span::styled("(sin filtro)", Style::new().fg(TENUE)),
                (false, true, true) => Span::styled(format!("‹ {valor} ›"), Style::new().fg(AMBAR).bold()),
                (false, _, _) => Span::styled(valor, Style::new().fg(AMBAR)),
            };
            Line::from(vec![
                Span::styled(if sel { "▸ " } else { "  " }, Style::new().fg(ROJO)),
                Span::styled(
                    format!("{:<16}", c.etiqueta()),
                    if sel {
                        Style::new().bold()
                    } else {
                        Style::new().fg(GRIS)
                    },
                ),
                mostrado,
                Span::styled(
                    if sel && !c.ayuda().starts_with('←') {
                        "▏"
                    } else {
                        ""
                    },
                    Style::new().fg(ROJO),
                ),
            ])
        })
        .collect();
    lineas.push(Line::raw(""));
    lineas.push(Line::styled(CampoFiltro::TODOS[campo].ayuda(), Style::new().fg(GRIS)));
    lineas.push(Line::styled(
        "Admin aplica a cada consulta solo los filtros que le corresponden y avisa cuáles no.",
        Style::new().fg(TENUE),
    ));
    lineas.push(Line::raw(""));
    lineas.push(atajos(&[
        ("↑↓", "campo"),
        ("Supr", "borrar"),
        ("Enter", "aplicar"),
        ("Esc", "cancelar"),
    ]));
    let bloque = marco("Filtros de las consultas", true).padding(Padding::new(2, 2, 1, 0));
    f.render_widget(Clear, area);
    f.render_widget(Paragraph::new(lineas).block(bloque).wrap(Wrap { trim: false }), area);
}

#[cfg(test)]
mod pruebas {
    use super::*;
    use crate::app::{Aviso, Estado};
    use crate::modelos::{Sesion, Usuario};
    use ratatui::backend::TestBackend;
    use ratatui::Terminal;
    use serde_json::Map;

    /// Dibuja la app en una terminal de 120×32 y devuelve el texto de la pantalla.
    fn pantalla(app: &App) -> String {
        let mut terminal = Terminal::new(TestBackend::new(120, 32)).unwrap();
        terminal.draw(|f| dibujar(f, app)).unwrap();
        let b = terminal.backend().buffer();
        (0..b.area.height)
            .map(|y| (0..b.area.width).map(|x| b[(x, y)].symbol()).collect::<String>())
            .collect::<Vec<_>>()
            .join("\n")
    }

    fn app_dentro() -> App {
        let mut app = App::nuevo("http://localhost:8003".into());
        app.pantalla = Pantalla::Principal;
        app.sesion = Some(Sesion {
            token: "a.b.c".into(),
            usuario: Usuario {
                id: "1".into(),
                nombre: "Administrador".into(),
                email: "admin@arqui.local".into(),
                rol: "admin".into(),
            },
        });
        app
    }

    #[test]
    fn el_login_oculta_la_contrasena() {
        let mut app = App::nuevo("http://localhost:8003".into());
        app.login.email = "admin@arqui.local".into();
        app.login.password = "secreta".into();
        let texto = pantalla(&app);
        assert!(texto.contains("admin@arqui.local"));
        assert!(texto.contains("•••••••"));
        assert!(!texto.contains("secreta"));
    }

    #[test]
    fn mientras_consulta_muestra_consultando_y_el_menu_sigue_ahi() {
        let mut app = app_dentro();
        app.panel = Panel::Resumen;
        app.resumen = Estado::Consultando {
            id: 1,
            desde: Instant::now(),
        };
        let texto = pantalla(&app);
        assert!(texto.contains("Consultando…"), "{texto}");
        assert!(texto.contains("Puedes seguir navegando"));
        assert!(texto.contains("3 Rentabilidad"));
    }

    #[test]
    fn el_resumen_muestra_cifras_y_filtros_aplicados() {
        let mut app = app_dentro();
        app.panel = Panel::Resumen;
        let mut aplicados = Map::new();
        aplicados.insert("modo".into(), "torneo".into());
        app.resumen = Estado::Listo(Consulta {
            tipo: "resumen_financiero".into(),
            filtros_aplicados: aplicados,
            filtros_no_aplicados: vec!["plantilla".into()],
            data: ResumenFinanciero {
                tokens_comprados: 1500,
                cop_comprados: 1_500_000,
                apostado_individual: 800,
                pagado_individual: 1000,
                inscripciones_cobradas: 400,
                inscripciones_devueltas: 0,
                premios_pagados: 360,
                ganancia_neta: -160,
                botin_en_juego: 0,
                apostado_pendiente: 0,
            },
        });
        let texto = pantalla(&app);
        assert!(texto.contains("1.500 (1.500.000 COP)"), "{texto}");
        assert!(texto.contains("-160"));
        assert!(texto.contains("Modo=torneo"));
        assert!(texto.contains("No aplican a esta consulta: Plantilla"));
    }

    #[test]
    fn el_popup_de_programar_muestra_la_carrera_y_el_error() {
        let mut app = app_dentro();
        let carrera = Carrera {
            carrera_id: "9165".into(),
            nombre: "Singapore 2023 - Race".into(),
            circuito: "Singapore".into(),
            temporada: Some(2023),
            total_vueltas: Some(62),
            hora_largada: None,
            estado: "descargada".into(),
            pilotos: 19,
        };
        app.carreras = Estado::Listo(vec![carrera.clone()]);
        app.popup = Some(Popup::Programar {
            carrera,
            minutos: "10".into(),
            editado: false,
            error: Some("Ya hay una simulación programada o en curso".into()),
            pendiente: None,
        });
        let texto = pantalla(&app);
        assert!(texto.contains("Programar simulación"));
        assert!(texto.contains("Singapore 2023 - Race"));
        assert!(texto.contains("[  10▏]"));
        assert!(texto.contains("Ya hay una simulación programada o en curso"));
    }

    #[test]
    fn el_aviso_reemplaza_los_atajos_del_pie() {
        let mut app = app_dentro();
        app.aviso = Some(Aviso {
            texto: "Simulación programada".into(),
            ok: true,
        });
        assert!(pantalla(&app).contains("✓ Simulación programada"));
    }
}
