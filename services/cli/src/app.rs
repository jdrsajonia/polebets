//! Estado de la CLI y lo que hace cada tecla (HU2-17).
//!
//! No dibuja ni hace red: `tecla` y `recibir` devuelven las peticiones que hay
//! que lanzar (`Envio`), y main las corre en tareas aparte. Mientras una
//! consulta espera, su panel queda en `Consultando` y la navegación sigue
//! libre. Cada petición lleva un id: si llega una respuesta que ya nadie
//! espera (el panel se recargó o cambiaron los filtros), se descarta.

use std::collections::HashMap;
use std::time::Instant;

use ratatui::crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use serde_json::{Map, Value};

use crate::api::ErrorApi;
use crate::formato;
use crate::modelos::{
    Carrera, Consulta, Horarios, Participacion, Rentabilidad, ResumenFinanciero, Sesion, SimulacionProgramada,
};

/// Minutos de anticipación (RF-22: [10] por defecto; Admin acepta 1 a 120).
pub const MINUTOS_POR_DEFECTO: u32 = 10;
pub const MINUTOS_MAX: u32 = 120;
/// Lo que se muestra al volver al login cuando Admin responde 401.
pub const MENSAJE_SESION_VENCIDA: &str = "La sesión venció. Vuelve a ingresar.";

/// Paneles del menú lateral, en orden.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Panel {
    Simulacion,
    Resumen,
    Rentabilidad,
    Horarios,
    Participacion,
}

impl Panel {
    pub const TODOS: [Panel; 5] = [
        Panel::Simulacion,
        Panel::Resumen,
        Panel::Rentabilidad,
        Panel::Horarios,
        Panel::Participacion,
    ];

    /// Nombre del panel en el menú y en el título.
    pub fn titulo(self) -> &'static str {
        match self {
            Panel::Simulacion => "Simulación",
            Panel::Resumen => "Resumen financiero",
            Panel::Rentabilidad => "Rentabilidad",
            Panel::Horarios => "Horarios",
            Panel::Participacion => "Participación",
        }
    }

    /// Nombre de la consulta en Admin (`query`); la simulación no es consulta.
    pub fn query(self) -> Option<&'static str> {
        match self {
            Panel::Simulacion => None,
            Panel::Resumen => Some("resumen_financiero"),
            Panel::Rentabilidad => Some("rentabilidad"),
            Panel::Horarios => Some("horarios"),
            Panel::Participacion => Some("participacion"),
        }
    }

    /// Posición en el menú (0..5).
    pub fn indice(self) -> usize {
        Panel::TODOS.iter().position(|p| *p == self).unwrap_or(0)
    }
}

/// Estado de los datos de un panel.
#[derive(Debug, Clone, Default)]
pub enum Estado<T> {
    #[default]
    SinCargar,
    /// Esperando la respuesta `id`, pedida en `desde`.
    Consultando {
        id: u64,
        desde: Instant,
    },
    Listo(T),
    Fallo(String),
}

impl<T> Estado<T> {
    /// true si este panel está esperando justo la respuesta `id`.
    fn espera(&self, id: u64) -> bool {
        matches!(self, Estado::Consultando { id: i, .. } if *i == id)
    }

    /// Hay que pedir los datos: nunca se pidieron, o se fuerza (si no hay ya una en curso).
    fn debe_cargar(&self, forzar: bool) -> bool {
        match self {
            Estado::SinCargar => true,
            Estado::Consultando { .. } => false,
            _ => forzar,
        }
    }

    /// Los datos, si ya llegaron.
    pub fn datos(&self) -> Option<&T> {
        match self {
            Estado::Listo(d) => Some(d),
            _ => None,
        }
    }
}

/// Lo que la CLI le pide a Admin.
#[derive(Debug, Clone, PartialEq)]
pub enum Peticion {
    Login {
        email: String,
        password: String,
    },
    Carreras {
        token: String,
    },
    Programar {
        token: String,
        race_id: String,
        minutos: u32,
    },
    Consulta {
        token: String,
        panel: Panel,
        params: Map<String, Value>,
    },
}

/// Una petición con su id, para emparejarla con la respuesta.
#[derive(Debug, Clone, PartialEq)]
pub struct Envio {
    pub id: u64,
    pub peticion: Peticion,
}

/// Lo que puede responder Admin, ya leído.
#[derive(Debug, Clone)]
pub enum Datos {
    Sesion(Sesion),
    Carreras(Vec<Carrera>),
    Programada(SimulacionProgramada),
    Resumen(Consulta<ResumenFinanciero>),
    Rentabilidad(Consulta<Rentabilidad>),
    Horarios(Consulta<Horarios>),
    Participacion(Consulta<Participacion>),
}

/// Respuesta de la petición `id`.
#[derive(Debug, Clone)]
pub struct Respuesta {
    pub id: u64,
    pub resultado: Result<Datos, ErrorApi>,
}

/// A quién le toca una respuesta.
#[derive(Debug, Clone, PartialEq)]
enum Destino {
    Login,
    Carreras,
    /// Programar la carrera con este nombre (para el aviso).
    Programar(String),
    Panel(Panel),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Pantalla {
    Login,
    Principal,
}

/// Dónde actúan las flechas: el menú de paneles o el contenido del panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Foco {
    Menu,
    Contenido,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum CampoLogin {
    #[default]
    Correo,
    Clave,
}

/// Formulario de inicio de sesión.
#[derive(Debug, Clone, Default)]
pub struct FormLogin {
    pub email: String,
    pub password: String,
    pub campo: CampoLogin,
    pub error: Option<String>,
    /// Id del login en curso: mientras exista se muestra «Ingresando…».
    pub pendiente: Option<u64>,
}

/// Filtro por tipo de apuesta (HU2-23).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TipoApuesta {
    #[default]
    Todas,
    Estatica,
    Dinamica,
}

/// Filtro por modo (HU2-23).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Modo {
    #[default]
    Todos,
    Individual,
    Torneo,
}

/// Filtros de las consultas (HU2-23, RF-29). Vacío = sin ese filtro.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Filtros {
    pub evento_id: String,
    pub fecha_inicio: String,
    pub fecha_fin: String,
    pub tipo_apuesta: TipoApuesta,
    pub modo: Modo,
    pub plantilla: String,
    pub top_n: String,
}

/// Campos del formulario de filtros, en orden.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CampoFiltro {
    Evento,
    Desde,
    Hasta,
    Tipo,
    Modo,
    Plantilla,
    TopN,
}

impl CampoFiltro {
    pub const TODOS: [CampoFiltro; 7] = [
        CampoFiltro::Evento,
        CampoFiltro::Desde,
        CampoFiltro::Hasta,
        CampoFiltro::Tipo,
        CampoFiltro::Modo,
        CampoFiltro::Plantilla,
        CampoFiltro::TopN,
    ];

    /// Nombre del campo en el formulario.
    pub fn etiqueta(self) -> &'static str {
        match self {
            CampoFiltro::Evento => "Evento",
            CampoFiltro::Desde => "Desde",
            CampoFiltro::Hasta => "Hasta",
            CampoFiltro::Tipo => "Tipo de apuesta",
            CampoFiltro::Modo => "Modo",
            CampoFiltro::Plantilla => "Plantilla",
            CampoFiltro::TopN => "Top N",
        }
    }

    /// Ayuda que se muestra con el campo seleccionado.
    pub fn ayuda(self) -> &'static str {
        match self {
            CampoFiltro::Evento => "id (UUID) del evento; lo ves en el panel Participación",
            CampoFiltro::Desde | CampoFiltro::Hasta => "fecha AAAA-MM-DD (o AAAA-MM-DDTHH:MM)",
            CampoFiltro::Tipo | CampoFiltro::Modo => "←/→ para cambiar",
            CampoFiltro::Plantilla => "p. ej. ganador, cambio_lider (solo Rentabilidad)",
            CampoFiltro::TopN => "cuántas plantillas en el ranking, 1 a 100 (solo Rentabilidad)",
        }
    }

    /// true si se edita escribiendo (los demás se cambian con ←/→).
    fn es_texto(self) -> bool {
        !matches!(self, CampoFiltro::Tipo | CampoFiltro::Modo)
    }
}

impl Filtros {
    /// Valor del campo tal como se muestra ("" = sin filtro).
    pub fn valor(&self, campo: CampoFiltro) -> String {
        match campo {
            CampoFiltro::Evento => self.evento_id.clone(),
            CampoFiltro::Desde => self.fecha_inicio.clone(),
            CampoFiltro::Hasta => self.fecha_fin.clone(),
            CampoFiltro::Tipo => match self.tipo_apuesta {
                TipoApuesta::Todas => String::new(),
                TipoApuesta::Estatica => "estatica".into(),
                TipoApuesta::Dinamica => "dinamica".into(),
            },
            CampoFiltro::Modo => match self.modo {
                Modo::Todos => String::new(),
                Modo::Individual => "individual".into(),
                Modo::Torneo => "torneo".into(),
            },
            CampoFiltro::Plantilla => self.plantilla.clone(),
            CampoFiltro::TopN => self.top_n.clone(),
        }
    }

    /// Texto editable del campo (None en los de ←/→).
    fn texto_mut(&mut self, campo: CampoFiltro) -> Option<&mut String> {
        match campo {
            CampoFiltro::Evento => Some(&mut self.evento_id),
            CampoFiltro::Desde => Some(&mut self.fecha_inicio),
            CampoFiltro::Hasta => Some(&mut self.fecha_fin),
            CampoFiltro::Plantilla => Some(&mut self.plantilla),
            CampoFiltro::TopN => Some(&mut self.top_n),
            CampoFiltro::Tipo | CampoFiltro::Modo => None,
        }
    }

    /// Avanza (o retrocede) la opción de un campo de ←/→.
    fn rotar(&mut self, campo: CampoFiltro, adelante: bool) {
        match campo {
            CampoFiltro::Tipo => {
                let ops = [TipoApuesta::Todas, TipoApuesta::Estatica, TipoApuesta::Dinamica];
                self.tipo_apuesta = ops[siguiente(ops.iter().position(|o| *o == self.tipo_apuesta), 3, adelante)];
            }
            CampoFiltro::Modo => {
                let ops = [Modo::Todos, Modo::Individual, Modo::Torneo];
                self.modo = ops[siguiente(ops.iter().position(|o| *o == self.modo), 3, adelante)];
            }
            _ => {}
        }
    }

    /// Deja el campo sin filtro.
    fn borrar(&mut self, campo: CampoFiltro) {
        match self.texto_mut(campo) {
            Some(texto) => texto.clear(),
            None if campo == CampoFiltro::Tipo => self.tipo_apuesta = TipoApuesta::Todas,
            None => self.modo = Modo::Todos,
        }
    }

    /// `params` para Admin con los filtros puestos (nombres de Admin, HU2-23).
    /// Se mandan todos: Admin responde cuáles aplicó a cada consulta.
    pub fn params(&self) -> Map<String, Value> {
        let mut params = Map::new();
        let claves = [
            (CampoFiltro::Evento, "evento_id"),
            (CampoFiltro::Desde, "fecha_inicio"),
            (CampoFiltro::Hasta, "fecha_fin"),
            (CampoFiltro::Tipo, "tipo_apuesta"),
            (CampoFiltro::Modo, "modo"),
            (CampoFiltro::Plantilla, "plantilla"),
        ];
        for (campo, clave) in claves {
            let valor = self.valor(campo);
            let valor = valor.trim();
            if !valor.is_empty() {
                params.insert(clave.into(), Value::String(valor.into()));
            }
        }
        if let Ok(n) = self.top_n.trim().parse::<u64>() {
            params.insert("top_n".into(), Value::from(n));
        }
        params
    }

    /// Filtros puestos como (etiqueta, valor), para mostrarlos.
    pub fn activos(&self) -> Vec<(&'static str, String)> {
        CampoFiltro::TODOS
            .iter()
            .map(|c| (c.etiqueta(), self.valor(*c).trim().to_string()))
            .filter(|(_, v)| !v.is_empty())
            .collect()
    }
}

/// Índice siguiente o anterior en una lista circular de `n`.
fn siguiente(actual: Option<usize>, n: usize, adelante: bool) -> usize {
    let i = actual.unwrap_or(0);
    if adelante {
        (i + 1) % n
    } else {
        (i + n - 1) % n
    }
}

/// Ventana emergente sobre la pantalla principal.
#[derive(Debug, Clone)]
pub enum Popup {
    /// HU2-19: elegir los minutos de anticipación y confirmar.
    Programar {
        carrera: Carrera,
        minutos: String,
        /// false = el próximo dígito reemplaza el valor en vez de agregarse.
        editado: bool,
        error: Option<String>,
        pendiente: Option<u64>,
    },
    /// HU2-23: editar los filtros de las consultas.
    Filtros { borrador: Filtros, campo: usize },
}

/// Mensaje de una línea en el pie (resultado de una acción).
#[derive(Debug, Clone, PartialEq)]
pub struct Aviso {
    pub texto: String,
    pub ok: bool,
}

/// Todo el estado de la CLI.
pub struct App {
    pub url: String,
    pub pantalla: Pantalla,
    pub login: FormLogin,
    pub sesion: Option<Sesion>,
    pub panel: Panel,
    pub foco: Foco,
    pub carreras: Estado<Vec<Carrera>>,
    pub carrera_sel: usize,
    pub resumen: Estado<Consulta<ResumenFinanciero>>,
    pub rentabilidad: Estado<Consulta<Rentabilidad>>,
    pub horarios: Estado<Consulta<Horarios>>,
    pub participacion: Estado<Consulta<Participacion>>,
    /// Fila seleccionada en la tabla del panel de consulta actual.
    pub fila: usize,
    pub filtros: Filtros,
    pub popup: Option<Popup>,
    pub aviso: Option<Aviso>,
    pub salir: bool,
    siguiente_id: u64,
    pendientes: HashMap<u64, Destino>,
}

impl App {
    /// CLI recién abierta: pantalla de login.
    pub fn nuevo(url: String) -> Self {
        App {
            url,
            pantalla: Pantalla::Login,
            login: FormLogin::default(),
            sesion: None,
            panel: Panel::Simulacion,
            foco: Foco::Menu,
            carreras: Estado::SinCargar,
            carrera_sel: 0,
            resumen: Estado::SinCargar,
            rentabilidad: Estado::SinCargar,
            horarios: Estado::SinCargar,
            participacion: Estado::SinCargar,
            fila: 0,
            filtros: Filtros::default(),
            popup: None,
            aviso: None,
            salir: false,
            siguiente_id: 1,
            pendientes: HashMap::new(),
        }
    }

    /// Atiende una tecla y devuelve las peticiones que hay que lanzar.
    pub fn tecla(&mut self, k: KeyEvent) -> Vec<Envio> {
        if k.modifiers.contains(KeyModifiers::CONTROL) && k.code == KeyCode::Char('c') {
            self.salir = true;
            return vec![];
        }
        match self.pantalla {
            Pantalla::Login => self.tecla_login(k),
            Pantalla::Principal => {
                self.aviso = None;
                if self.popup.is_some() {
                    self.tecla_popup(k)
                } else {
                    self.tecla_principal(k)
                }
            }
        }
    }

    /// Recibe la respuesta de una petición; puede pedir más (p. ej. tras el login).
    pub fn recibir(&mut self, r: Respuesta) -> Vec<Envio> {
        let Some(destino) = self.pendientes.remove(&r.id) else {
            return vec![]; // de una sesión anterior: nadie la espera
        };
        if destino != Destino::Login && matches!(r.resultado, Err(ErrorApi::SinSesion(_))) {
            self.cerrar_sesion(Some(MENSAJE_SESION_VENCIDA.into()));
            return vec![];
        }
        match destino {
            Destino::Login => self.recibir_login(r),
            Destino::Carreras => {
                if self.carreras.espera(r.id) {
                    self.carreras = match r.resultado {
                        Ok(Datos::Carreras(c)) => {
                            self.carrera_sel = self.carrera_sel.min(c.len().saturating_sub(1));
                            Estado::Listo(c)
                        }
                        Ok(_) => Estado::Fallo("Respuesta inesperada de Admin".into()),
                        Err(e) => Estado::Fallo(e.mensaje()),
                    };
                }
                vec![]
            }
            Destino::Programar(nombre) => {
                self.recibir_programar(r, &nombre);
                vec![]
            }
            Destino::Panel(panel) => {
                self.recibir_consulta(panel, r);
                vec![]
            }
        }
    }

    /// Respuesta del login: entra y carga el primer panel, o muestra el error.
    fn recibir_login(&mut self, r: Respuesta) -> Vec<Envio> {
        if self.login.pendiente != Some(r.id) {
            return vec![];
        }
        self.login.pendiente = None;
        match r.resultado {
            Ok(Datos::Sesion(s)) => {
                self.sesion = Some(s);
                self.pantalla = Pantalla::Principal;
                self.login.password.clear();
                self.login.error = None;
                self.foco = Foco::Menu;
                self.cargar(false)
            }
            Ok(_) => {
                self.login.error = Some("Respuesta inesperada de Admin".into());
                vec![]
            }
            Err(e) => {
                self.login.error = Some(e.mensaje());
                vec![]
            }
        }
    }

    /// Respuesta de programar una simulación: cierra el popup o muestra el error en él.
    fn recibir_programar(&mut self, r: Respuesta, nombre: &str) {
        let en_popup = matches!(&self.popup, Some(Popup::Programar { pendiente: Some(p), .. }) if *p == r.id);
        match r.resultado {
            Ok(Datos::Programada(s)) => {
                if en_popup {
                    self.popup = None;
                }
                let hora = formato::hora_colombia(&s.hora_largada).unwrap_or(s.hora_largada);
                self.aviso = Some(Aviso {
                    texto: format!("Simulación programada: {nombre} larga a las {hora} (hora Colombia)"),
                    ok: true,
                });
            }
            resultado => {
                let mensaje = match resultado {
                    Err(e) => e.mensaje(),
                    Ok(_) => "Respuesta inesperada de Admin".into(),
                };
                match &mut self.popup {
                    Some(Popup::Programar { error, pendiente, .. }) if en_popup => {
                        *error = Some(mensaje);
                        *pendiente = None;
                    }
                    _ => {
                        self.aviso = Some(Aviso {
                            texto: mensaje,
                            ok: false,
                        })
                    }
                }
            }
        }
    }

    /// Guarda los datos (o el error) de una consulta si su panel aún los espera.
    fn recibir_consulta(&mut self, panel: Panel, r: Respuesta) {
        let id = r.id;
        match (panel, r.resultado) {
            (Panel::Resumen, Ok(Datos::Resumen(c))) if self.resumen.espera(id) => self.resumen = Estado::Listo(c),
            (Panel::Rentabilidad, Ok(Datos::Rentabilidad(c))) if self.rentabilidad.espera(id) => {
                self.rentabilidad = Estado::Listo(c)
            }
            (Panel::Horarios, Ok(Datos::Horarios(c))) if self.horarios.espera(id) => self.horarios = Estado::Listo(c),
            (Panel::Participacion, Ok(Datos::Participacion(c))) if self.participacion.espera(id) => {
                self.participacion = Estado::Listo(c)
            }
            (panel, Err(e)) => self.marcar_fallo(panel, id, e.mensaje()),
            _ => {}
        }
        if panel == self.panel {
            self.fila = self.fila.min(self.filas_panel().saturating_sub(1));
        }
    }

    // ------------------------------------------------------------------
    // Teclas
    // ------------------------------------------------------------------

    /// Pantalla de login: escribir, cambiar de campo, ingresar o salir.
    fn tecla_login(&mut self, k: KeyEvent) -> Vec<Envio> {
        match k.code {
            KeyCode::Esc => self.salir = true,
            KeyCode::Tab | KeyCode::BackTab | KeyCode::Up | KeyCode::Down => {
                self.login.campo = match self.login.campo {
                    CampoLogin::Correo => CampoLogin::Clave,
                    CampoLogin::Clave => CampoLogin::Correo,
                };
            }
            KeyCode::Enter => return self.enviar_login(),
            KeyCode::Backspace => {
                self.campo_login_mut().pop();
            }
            KeyCode::Char(c) if !k.modifiers.contains(KeyModifiers::CONTROL) => self.campo_login_mut().push(c),
            _ => {}
        }
        vec![]
    }

    /// Texto del campo de login activo.
    fn campo_login_mut(&mut self) -> &mut String {
        match self.login.campo {
            CampoLogin::Correo => &mut self.login.email,
            CampoLogin::Clave => &mut self.login.password,
        }
    }

    /// Enter en el login: pasa a la contraseña o manda las credenciales.
    fn enviar_login(&mut self) -> Vec<Envio> {
        if self.login.pendiente.is_some() {
            return vec![];
        }
        if self.login.campo == CampoLogin::Correo && self.login.password.is_empty() {
            self.login.campo = CampoLogin::Clave;
            return vec![];
        }
        if self.login.email.trim().is_empty() || self.login.password.is_empty() {
            self.login.error = Some("Escribe el correo y la contraseña".into());
            return vec![];
        }
        let envio = self.pedir(
            Destino::Login,
            Peticion::Login {
                email: self.login.email.trim().to_string(),
                password: self.login.password.clone(),
            },
        );
        self.login.pendiente = Some(envio.id);
        self.login.error = None;
        vec![envio]
    }

    /// Pantalla principal sin popup: navegar, recargar, filtros, programar, salir.
    fn tecla_principal(&mut self, k: KeyEvent) -> Vec<Envio> {
        match k.code {
            KeyCode::Char('q') => self.salir = true,
            KeyCode::Char('s') => self.cerrar_sesion(None),
            KeyCode::Char(d @ '1'..='5') => {
                let i = d as usize - '1' as usize;
                return self.elegir_panel(Panel::TODOS[i]);
            }
            KeyCode::Up | KeyCode::Char('k') => return self.mover(-1),
            KeyCode::Down | KeyCode::Char('j') => return self.mover(1),
            KeyCode::PageUp => return self.mover(-10),
            KeyCode::PageDown => return self.mover(10),
            KeyCode::Enter | KeyCode::Right | KeyCode::Char('l') | KeyCode::Tab => {
                if self.foco == Foco::Menu {
                    self.foco = Foco::Contenido;
                } else if k.code == KeyCode::Enter && self.panel == Panel::Simulacion {
                    self.abrir_programar();
                }
            }
            KeyCode::Char('p') if self.panel == Panel::Simulacion => self.abrir_programar(),
            KeyCode::Left | KeyCode::Char('h') | KeyCode::Esc | KeyCode::BackTab => self.foco = Foco::Menu,
            KeyCode::Char('r') => return self.cargar(true),
            KeyCode::Char('f') => {
                self.popup = Some(Popup::Filtros {
                    borrador: self.filtros.clone(),
                    campo: 0,
                });
            }
            KeyCode::Char('c') => {
                if self.filtros == Filtros::default() {
                    self.aviso = Some(Aviso {
                        texto: "No hay filtros activos".into(),
                        ok: true,
                    });
                } else {
                    return self.aplicar_filtros(Filtros::default());
                }
            }
            _ => {}
        }
        vec![]
    }

    /// ↑/↓: en el menú cambia de panel; en el contenido mueve la fila.
    fn mover(&mut self, delta: i64) -> Vec<Envio> {
        if self.foco == Foco::Menu {
            let i = (self.panel.indice() as i64 + delta.signum()).rem_euclid(Panel::TODOS.len() as i64);
            return self.elegir_panel(Panel::TODOS[i as usize]);
        }
        let total = self.filas_panel();
        let actual = if self.panel == Panel::Simulacion {
            self.carrera_sel
        } else {
            self.fila
        };
        let nueva = (actual as i64 + delta).clamp(0, total.saturating_sub(1) as i64) as usize;
        if self.panel == Panel::Simulacion {
            self.carrera_sel = nueva;
        } else {
            self.fila = nueva;
        }
        vec![]
    }

    /// Cuántas filas tiene la tabla principal del panel actual.
    fn filas_panel(&self) -> usize {
        match self.panel {
            Panel::Simulacion => self.carreras.datos().map_or(0, |c| c.len()),
            Panel::Resumen => 0,
            Panel::Rentabilidad => self.rentabilidad.datos().map_or(0, |c| c.data.filas.len()),
            Panel::Horarios => self
                .horarios
                .datos()
                .map_or(0, |c| c.data.por_hora.len().max(c.data.por_vuelta.len())),
            Panel::Participacion => self.participacion.datos().map_or(0, |c| c.data.eventos.len()),
        }
    }

    /// Cambia de panel y lo carga si nunca se cargó.
    fn elegir_panel(&mut self, panel: Panel) -> Vec<Envio> {
        if panel != self.panel {
            self.panel = panel;
            self.fila = 0;
        }
        self.cargar(false)
    }

    /// Abre el popup para programar la carrera seleccionada (HU2-19).
    fn abrir_programar(&mut self) {
        match self.carreras.datos().and_then(|c| c.get(self.carrera_sel)) {
            Some(carrera) => {
                self.popup = Some(Popup::Programar {
                    carrera: carrera.clone(),
                    minutos: MINUTOS_POR_DEFECTO.to_string(),
                    editado: false,
                    error: None,
                    pendiente: None,
                });
            }
            None => {
                self.aviso = Some(Aviso {
                    texto: "No hay una carrera seleccionada".into(),
                    ok: false,
                })
            }
        }
    }

    /// Teclas dentro de un popup.
    fn tecla_popup(&mut self, k: KeyEvent) -> Vec<Envio> {
        let Some(popup) = self.popup.take() else { return vec![] };
        match popup {
            Popup::Programar {
                carrera,
                minutos,
                editado,
                error,
                pendiente,
            } => self.tecla_programar(k, carrera, minutos, editado, error, pendiente),
            Popup::Filtros { borrador, campo } => self.tecla_filtros(k, borrador, campo),
        }
    }

    /// Popup de programar: ajustar minutos, confirmar o cancelar.
    fn tecla_programar(
        &mut self,
        k: KeyEvent,
        carrera: Carrera,
        mut minutos: String,
        mut editado: bool,
        mut error: Option<String>,
        mut pendiente: Option<u64>,
    ) -> Vec<Envio> {
        let mut envios = vec![];
        let ajustar = |m: &str, d: i64| {
            let actual = m.parse::<i64>().unwrap_or(MINUTOS_POR_DEFECTO as i64);
            (actual + d).clamp(1, MINUTOS_MAX as i64).to_string()
        };
        match k.code {
            KeyCode::Esc => return vec![], // el popup ya se sacó: queda cerrado
            KeyCode::Up | KeyCode::Right | KeyCode::Char('+') => (minutos, editado) = (ajustar(&minutos, 1), false),
            KeyCode::Down | KeyCode::Left | KeyCode::Char('-') => (minutos, editado) = (ajustar(&minutos, -1), false),
            KeyCode::PageUp => (minutos, editado) = (ajustar(&minutos, 10), false),
            KeyCode::PageDown => (minutos, editado) = (ajustar(&minutos, -10), false),
            KeyCode::Char(d) if d.is_ascii_digit() => {
                let mut nuevo = if editado { minutos.clone() } else { String::new() };
                nuevo.push(d);
                if nuevo.parse::<u32>().is_ok_and(|m| m <= MINUTOS_MAX) {
                    minutos = nuevo;
                }
                editado = true;
            }
            KeyCode::Backspace => {
                minutos.pop();
                editado = true;
            }
            KeyCode::Enter if pendiente.is_none() => {
                match (minutos.parse::<u32>(), self.sesion.as_ref().map(|s| s.token.clone())) {
                    (Ok(m), Some(token)) if (1..=MINUTOS_MAX).contains(&m) => {
                        let envio = self.pedir(
                            Destino::Programar(carrera.nombre.clone()),
                            Peticion::Programar {
                                token,
                                race_id: carrera.carrera_id.clone(),
                                minutos: m,
                            },
                        );
                        pendiente = Some(envio.id);
                        error = None;
                        envios.push(envio);
                    }
                    _ => error = Some(format!("Los minutos van de 1 a {MINUTOS_MAX}")),
                }
            }
            _ => {}
        }
        self.popup = Some(Popup::Programar {
            carrera,
            minutos,
            editado,
            error,
            pendiente,
        });
        envios
    }

    /// Popup de filtros: moverse, editar, aplicar o cancelar.
    fn tecla_filtros(&mut self, k: KeyEvent, mut borrador: Filtros, mut campo: usize) -> Vec<Envio> {
        let n = CampoFiltro::TODOS.len();
        let actual = CampoFiltro::TODOS[campo];
        match k.code {
            KeyCode::Esc => return vec![],
            KeyCode::Enter => return self.aplicar_filtros(borrador),
            KeyCode::Up | KeyCode::BackTab => campo = (campo + n - 1) % n,
            KeyCode::Down | KeyCode::Tab => campo = (campo + 1) % n,
            KeyCode::Left => borrador.rotar(actual, false),
            KeyCode::Right | KeyCode::Char(' ') if !actual.es_texto() => borrador.rotar(actual, true),
            KeyCode::Delete => borrador.borrar(actual),
            KeyCode::Backspace => {
                if let Some(texto) = borrador.texto_mut(actual) {
                    texto.pop();
                } else {
                    borrador.borrar(actual);
                }
            }
            KeyCode::Char(c) => {
                if let Some(texto) = borrador.texto_mut(actual) {
                    // Top N solo admite dígitos (hasta 3).
                    if actual != CampoFiltro::TopN || (c.is_ascii_digit() && texto.len() < 3) {
                        texto.push(c);
                    }
                }
            }
            _ => {}
        }
        self.popup = Some(Popup::Filtros { borrador, campo });
        vec![]
    }

    /// Cambia los filtros: las consultas viejas dejan de valer y se repite la del panel actual.
    fn aplicar_filtros(&mut self, nuevos: Filtros) -> Vec<Envio> {
        if nuevos == self.filtros {
            return vec![];
        }
        self.filtros = nuevos;
        self.resumen = Estado::SinCargar;
        self.rentabilidad = Estado::SinCargar;
        self.horarios = Estado::SinCargar;
        self.participacion = Estado::SinCargar;
        self.fila = 0;
        self.cargar(false)
    }

    // ------------------------------------------------------------------
    // Peticiones
    // ------------------------------------------------------------------

    /// Pide los datos del panel actual si hace falta (`forzar` = tecla r).
    fn cargar(&mut self, forzar: bool) -> Vec<Envio> {
        let Some(token) = self.sesion.as_ref().map(|s| s.token.clone()) else {
            return vec![];
        };
        let panel = self.panel;
        let debe = match panel {
            Panel::Simulacion => self.carreras.debe_cargar(forzar),
            Panel::Resumen => self.resumen.debe_cargar(forzar),
            Panel::Rentabilidad => self.rentabilidad.debe_cargar(forzar),
            Panel::Horarios => self.horarios.debe_cargar(forzar),
            Panel::Participacion => self.participacion.debe_cargar(forzar),
        };
        if !debe {
            return vec![];
        }
        let envio = if panel == Panel::Simulacion {
            self.pedir(Destino::Carreras, Peticion::Carreras { token })
        } else {
            self.pedir(
                Destino::Panel(panel),
                Peticion::Consulta {
                    token,
                    panel,
                    params: self.filtros.params(),
                },
            )
        };
        let (id, desde) = (envio.id, Instant::now());
        match panel {
            Panel::Simulacion => self.carreras = Estado::Consultando { id, desde },
            Panel::Resumen => self.resumen = Estado::Consultando { id, desde },
            Panel::Rentabilidad => self.rentabilidad = Estado::Consultando { id, desde },
            Panel::Horarios => self.horarios = Estado::Consultando { id, desde },
            Panel::Participacion => self.participacion = Estado::Consultando { id, desde },
        }
        vec![envio]
    }

    /// Pone en Fallo el panel si todavía esperaba la respuesta `id`.
    fn marcar_fallo(&mut self, panel: Panel, id: u64, mensaje: String) {
        match panel {
            Panel::Simulacion if self.carreras.espera(id) => self.carreras = Estado::Fallo(mensaje),
            Panel::Resumen if self.resumen.espera(id) => self.resumen = Estado::Fallo(mensaje),
            Panel::Rentabilidad if self.rentabilidad.espera(id) => self.rentabilidad = Estado::Fallo(mensaje),
            Panel::Horarios if self.horarios.espera(id) => self.horarios = Estado::Fallo(mensaje),
            Panel::Participacion if self.participacion.espera(id) => self.participacion = Estado::Fallo(mensaje),
            _ => {}
        }
    }

    /// Registra una petición nueva y le da id.
    fn pedir(&mut self, destino: Destino, peticion: Peticion) -> Envio {
        let id = self.siguiente_id;
        self.siguiente_id += 1;
        self.pendientes.insert(id, destino);
        Envio { id, peticion }
    }

    /// Vuelve al login y olvida todo lo de la sesión (tecla s o 401 de Admin).
    fn cerrar_sesion(&mut self, mensaje: Option<String>) {
        self.sesion = None;
        self.pantalla = Pantalla::Login;
        self.login.error = mensaje;
        self.login.pendiente = None;
        self.login.campo = CampoLogin::Clave;
        self.popup = None;
        self.panel = Panel::Simulacion;
        self.foco = Foco::Menu;
        self.carreras = Estado::SinCargar;
        self.resumen = Estado::SinCargar;
        self.rentabilidad = Estado::SinCargar;
        self.horarios = Estado::SinCargar;
        self.participacion = Estado::SinCargar;
        self.pendientes.clear();
    }
}

#[cfg(test)]
mod pruebas {
    use super::*;
    use crate::modelos::Usuario;

    // --- Ayudas ---
    fn tecla(app: &mut App, codigo: KeyCode) -> Vec<Envio> {
        app.tecla(KeyEvent::from(codigo))
    }

    fn escribir(app: &mut App, texto: &str) {
        for c in texto.chars() {
            tecla(app, KeyCode::Char(c));
        }
    }

    fn sesion() -> Sesion {
        Sesion {
            token: "a.b.c".into(),
            usuario: Usuario {
                id: "1".into(),
                nombre: "Admin".into(),
                email: "admin@arqui.local".into(),
                rol: "admin".into(),
            },
        }
    }

    fn carrera(id: &str) -> Carrera {
        Carrera {
            carrera_id: id.into(),
            nombre: format!("Carrera {id}"),
            circuito: String::new(),
            temporada: None,
            total_vueltas: None,
            hora_largada: None,
            estado: "descargada".into(),
            pilotos: 20,
        }
    }

    fn consulta<T>(data: T) -> Consulta<T> {
        Consulta {
            tipo: "x".into(),
            filtros_aplicados: Map::new(),
            filtros_no_aplicados: vec![],
            data,
        }
    }

    fn resumen() -> ResumenFinanciero {
        ResumenFinanciero {
            tokens_comprados: 50,
            cop_comprados: 50_000,
            apostado_individual: 0,
            pagado_individual: 0,
            inscripciones_cobradas: 0,
            inscripciones_devueltas: 0,
            premios_pagados: 0,
            ganancia_neta: 0,
            botin_en_juego: 0,
            apostado_pendiente: 0,
        }
    }

    /// App ya dentro, con las carreras cargadas.
    fn app_dentro() -> App {
        let mut app = App::nuevo("http://admin".into());
        escribir(&mut app, "admin@arqui.local");
        tecla(&mut app, KeyCode::Tab);
        escribir(&mut app, "clave");
        let login = tecla(&mut app, KeyCode::Enter).remove(0);
        let carreras = app
            .recibir(Respuesta {
                id: login.id,
                resultado: Ok(Datos::Sesion(sesion())),
            })
            .remove(0);
        app.recibir(Respuesta {
            id: carreras.id,
            resultado: Ok(Datos::Carreras(vec![carrera("9165"), carrera("9158")])),
        });
        app
    }

    // --- Login (RF-21) ---
    #[test]
    fn el_login_manda_las_credenciales_y_al_entrar_carga_las_carreras() {
        let mut app = App::nuevo("http://admin".into());
        escribir(&mut app, "admin@arqui.local");
        assert!(
            tecla(&mut app, KeyCode::Enter).is_empty(),
            "Enter en el correo pasa a la contraseña"
        );
        escribir(&mut app, "clave");
        let envios = tecla(&mut app, KeyCode::Enter);
        assert_eq!(
            envios[0].peticion,
            Peticion::Login {
                email: "admin@arqui.local".into(),
                password: "clave".into()
            }
        );
        assert_eq!(app.login.pendiente, Some(envios[0].id));

        let siguientes = app.recibir(Respuesta {
            id: envios[0].id,
            resultado: Ok(Datos::Sesion(sesion())),
        });
        assert_eq!(app.pantalla, Pantalla::Principal);
        assert!(app.login.password.is_empty(), "la contraseña no queda en memoria");
        assert_eq!(siguientes[0].peticion, Peticion::Carreras { token: "a.b.c".into() });
        assert!(matches!(app.carreras, Estado::Consultando { .. }));
    }

    #[test]
    fn un_jugador_ve_el_rechazo_y_sigue_en_el_login() {
        let mut app = App::nuevo("http://admin".into());
        escribir(&mut app, "ana@correo.co");
        tecla(&mut app, KeyCode::Tab);
        escribir(&mut app, "clave");
        let id = tecla(&mut app, KeyCode::Enter)[0].id;
        app.recibir(Respuesta {
            id,
            resultado: Err(ErrorApi::Prohibido(
                "Solo los administradores pueden usar la CLI".into(),
            )),
        });
        assert_eq!(app.pantalla, Pantalla::Login);
        assert_eq!(
            app.login.error.as_deref(),
            Some("Solo los administradores pueden usar la CLI")
        );
        assert_eq!(app.login.pendiente, None);
    }

    #[test]
    fn sin_contrasena_no_se_manda_nada() {
        let mut app = App::nuevo("http://admin".into());
        tecla(&mut app, KeyCode::Tab);
        assert!(tecla(&mut app, KeyCode::Enter).is_empty());
        assert_eq!(app.login.error.as_deref(), Some("Escribe el correo y la contraseña"));
    }

    // --- Navegación sin bloquear (criterio «Consultando…») ---
    #[test]
    fn mientras_una_consulta_espera_se_puede_navegar_y_pedir_otra() {
        let mut app = app_dentro();
        let pedido = tecla(&mut app, KeyCode::Char('2'));
        assert!(matches!(app.resumen, Estado::Consultando { .. }));
        // Sin esperar la respuesta, el admin baja a Rentabilidad: se pide en paralelo.
        let rentab = tecla(&mut app, KeyCode::Down);
        assert_eq!(app.panel, Panel::Rentabilidad);
        assert!(matches!(
            rentab[0].peticion,
            Peticion::Consulta {
                panel: Panel::Rentabilidad,
                ..
            }
        ));
        // La respuesta del resumen llega después y queda guardada en su panel.
        app.recibir(Respuesta {
            id: pedido[0].id,
            resultado: Ok(Datos::Resumen(consulta(resumen()))),
        });
        assert_eq!(app.resumen.datos().unwrap().data.cop_comprados, 50_000);
        assert!(matches!(app.rentabilidad, Estado::Consultando { .. }));
    }

    #[test]
    fn volver_a_un_panel_cargado_no_lo_pide_otra_vez_pero_r_si() {
        let mut app = app_dentro();
        let id = tecla(&mut app, KeyCode::Char('2'))[0].id;
        app.recibir(Respuesta {
            id,
            resultado: Ok(Datos::Resumen(consulta(resumen()))),
        });
        tecla(&mut app, KeyCode::Char('1'));
        assert!(tecla(&mut app, KeyCode::Char('2')).is_empty());
        assert_eq!(tecla(&mut app, KeyCode::Char('r')).len(), 1);
        assert!(
            tecla(&mut app, KeyCode::Char('r')).is_empty(),
            "no se repite mientras espera"
        );
    }

    #[test]
    fn el_tardo_demasiado_queda_en_el_panel() {
        let mut app = app_dentro();
        let id = tecla(&mut app, KeyCode::Char('3'))[0].id;
        let error = ErrorApi::TardoDemasiado("La consulta tardó demasiado: Pagos no respondió en 10 s".into());
        app.recibir(Respuesta {
            id,
            resultado: Err(error),
        });
        assert!(matches!(&app.rentabilidad, Estado::Fallo(m) if m.contains("tardó demasiado")));
        assert_eq!(app.pantalla, Pantalla::Principal);
    }

    #[test]
    fn un_401_en_cualquier_panel_vuelve_al_login() {
        let mut app = app_dentro();
        let id = tecla(&mut app, KeyCode::Char('4'))[0].id;
        app.recibir(Respuesta {
            id,
            resultado: Err(ErrorApi::SinSesion("vencida".into())),
        });
        assert_eq!(app.pantalla, Pantalla::Login);
        assert!(app.sesion.is_none());
        assert_eq!(app.login.error.as_deref(), Some(MENSAJE_SESION_VENCIDA));
    }

    // --- Filtros (HU2-23) ---
    #[test]
    fn aplicar_filtros_repite_la_consulta_y_descarta_la_respuesta_vieja() {
        let mut app = app_dentro();
        let vieja = tecla(&mut app, KeyCode::Char('2')).remove(0);
        tecla(&mut app, KeyCode::Char('f'));
        for _ in 0..4 {
            tecla(&mut app, KeyCode::Down); // hasta «Modo»
        }
        tecla(&mut app, KeyCode::Right); // individual
        tecla(&mut app, KeyCode::Right); // torneo
        let nueva = tecla(&mut app, KeyCode::Enter).remove(0);
        match &nueva.peticion {
            Peticion::Consulta {
                panel: Panel::Resumen,
                params,
                ..
            } => assert_eq!(params["modo"], "torneo"),
            otra => panic!("se esperaba la consulta con filtros: {otra:?}"),
        }
        // La respuesta sin filtros llega tarde: no pisa la nueva.
        app.recibir(Respuesta {
            id: vieja.id,
            resultado: Ok(Datos::Resumen(consulta(resumen()))),
        });
        assert!(matches!(app.resumen, Estado::Consultando { id, .. } if id == nueva.id));
    }

    #[test]
    fn los_params_solo_llevan_lo_que_se_lleno() {
        let filtros = Filtros {
            fecha_inicio: " 2026-11-03 ".into(),
            tipo_apuesta: TipoApuesta::Dinamica,
            top_n: "5".into(),
            ..Filtros::default()
        };
        let params = filtros.params();
        assert_eq!(params.len(), 3);
        assert_eq!(params["fecha_inicio"], "2026-11-03");
        assert_eq!(params["tipo_apuesta"], "dinamica");
        assert_eq!(params["top_n"], 5);
    }

    #[test]
    fn limpiar_filtros_con_c() {
        let mut app = app_dentro();
        app.filtros.modo = Modo::Torneo;
        tecla(&mut app, KeyCode::Char('2'));
        let envios = tecla(&mut app, KeyCode::Char('c'));
        assert_eq!(app.filtros, Filtros::default());
        assert!(matches!(&envios[0].peticion, Peticion::Consulta { params, .. } if params.is_empty()));
    }

    // --- Simulación (HU2-19) ---
    #[test]
    fn programar_la_carrera_seleccionada_con_sus_minutos() {
        let mut app = app_dentro();
        tecla(&mut app, KeyCode::Enter); // foco al contenido
        tecla(&mut app, KeyCode::Down); // segunda carrera
        tecla(&mut app, KeyCode::Enter); // abre el popup
        escribir(&mut app, "15");
        let envio = tecla(&mut app, KeyCode::Enter).remove(0);
        assert_eq!(
            envio.peticion,
            Peticion::Programar {
                token: "a.b.c".into(),
                race_id: "9158".into(),
                minutos: 15
            }
        );

        let programada = SimulacionProgramada {
            simulacion_id: "s1".into(),
            hora_largada: "2026-11-03T21:00:00Z".into(),
        };
        app.recibir(Respuesta {
            id: envio.id,
            resultado: Ok(Datos::Programada(programada)),
        });
        assert!(app.popup.is_none());
        let aviso = app.aviso.clone().unwrap();
        assert!(aviso.ok);
        assert!(
            aviso.texto.contains("Carrera 9158") && aviso.texto.contains("16:00:00"),
            "{}",
            aviso.texto
        );
    }

    #[test]
    fn el_409_se_muestra_en_el_popup() {
        let mut app = app_dentro();
        tecla(&mut app, KeyCode::Char('p'));
        let id = tecla(&mut app, KeyCode::Enter)[0].id;
        let error = ErrorApi::Servidor {
            estado: 409,
            mensaje: "Ya hay una simulación programada o en curso".into(),
        };
        app.recibir(Respuesta {
            id,
            resultado: Err(error),
        });
        match &app.popup {
            Some(Popup::Programar {
                error: Some(m),
                pendiente: None,
                ..
            }) => assert!(m.starts_with("Ya hay")),
            otro => panic!("el popup debía mostrar el error: {otro:?}"),
        }
    }

    #[test]
    fn los_minutos_no_pasan_de_120_ni_bajan_de_1() {
        let mut app = app_dentro();
        tecla(&mut app, KeyCode::Char('p'));
        escribir(&mut app, "999"); // 9, 99 y el tercer 9 (999) se ignora
        let minutos = |app: &App| match &app.popup {
            Some(Popup::Programar { minutos, .. }) => minutos.clone(),
            _ => panic!(),
        };
        assert_eq!(minutos(&app), "99");
        for _ in 0..3 {
            tecla(&mut app, KeyCode::PageUp); // 109, 119 y el tope 120
        }
        assert_eq!(minutos(&app), "120");
        for _ in 0..13 {
            tecla(&mut app, KeyCode::PageDown);
        }
        assert_eq!(minutos(&app), "1");
    }

    #[test]
    fn cerrar_sesion_ignora_las_respuestas_que_llegan_despues() {
        let mut app = app_dentro();
        let id = tecla(&mut app, KeyCode::Char('2'))[0].id;
        tecla(&mut app, KeyCode::Char('s'));
        assert_eq!(app.pantalla, Pantalla::Login);
        app.recibir(Respuesta {
            id,
            resultado: Ok(Datos::Resumen(consulta(resumen()))),
        });
        assert!(matches!(app.resumen, Estado::SinCargar));
    }
}
