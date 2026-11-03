//! CLI del administrador de PoleBet (HU2-17).
//!
//!   api        cliente REST de Admin (única puerta de la CLI)
//!   modelos    respuestas de Admin en JSON
//!   app        estado y teclas; no dibuja ni hace red (se prueba solo)
//!   ejecutor   corre cada petición de la app contra Admin
//!   ui         dibuja el estado con ratatui
//!   formato    números y horas para la pantalla

pub mod api;
pub mod app;
pub mod ejecutor;
pub mod formato;
pub mod modelos;
pub mod ui;
