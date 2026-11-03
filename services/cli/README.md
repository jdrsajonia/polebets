# CLI del administrador (Rust) — HU2-17

Interfaz de terminal al estilo lazydocker para el administrador de PoleBet. Habla **solo
con Admin** (puerto 8003): nunca con Pagos, el Ingestor ni OpenF1. No es un servicio del
stack sino el cliente del administrador (componente de presentación, RNF-02).

```
 PoleBet  CLI del administrador                     Administrador · admin@arqui.local · http://admin:8003
╭ Paneles ─────────────────╮╭ Rentabilidad ───────────────────────────────────────────────────────────╮
│  1 Simulación            ││ Filtros activos: Tipo=dinamica                                          │
│  2 Resumen financiero    ││ No aplican a esta consulta: Modo                                        │
│▸ 3 Rentabilidad ⠹        ││ ⠹ Consultando… 2 s                                                      │
│  4 Horarios              ││ Puedes seguir navegando mientras llega la respuesta.                    │
│  5 Participación         ││                                                                         │
╰──────────────────────────╯│                                                                         │
╭ Filtros ─────────────────╮│                                                                         │
│Tipo de apuesta: dinamica ││                                                                         │
│Modo: torneo              ││                                                                         │
╰──────────────────────────╯╰─────────────────────────────────────────────────────────────────────────╯
 ↑↓ panel   → entrar   1-5 ir a   r recargar   f filtros   c limpiar   s cerrar sesión   q salir
```

## Correr

Con el stack levantado (`docker compose up -d`), desde la raíz del repo:

```bash
docker compose run --rm cli
```

El servicio `cli` tiene el perfil `cli`, así que `docker compose up` no lo levanta y siguen
siendo 14 contenedores. Dentro de Docker llega a Admin por la red `borde`
(`http://admin:8003`).

Con Rust instalado (1.87 o superior), sin Docker:

```bash
cd services/cli
cargo run --release                                  # Admin en http://localhost:8003
cargo run --release -- --url http://otra-maquina:8003
```

La URL sale de `--url`, de `POLEBET_ADMIN_URL` o, si no hay ninguna, de
`http://localhost:8003`.

Para entrar se usa una cuenta con rol `admin`. La inicial es `ADMIN_EMAIL` /
`ADMIN_PASSWORD` del `.env`.

## Teclas

| Tecla | Qué hace |
| --- | --- |
| `↑` `↓` (o `k` `j`) | En el menú: cambia de panel. En el contenido: mueve la fila |
| `1`–`5` | Va directo a un panel |
| `→` `Enter` `Tab` | Pasa del menú al contenido |
| `←` `Esc` | Vuelve al menú |
| `Enter` / `p` (en Simulación) | Programa la carrera seleccionada |
| `r` | Repite la consulta del panel |
| `f` | Edita los filtros · `c` los limpia |
| `s` | Cierra la sesión |
| `q` / `Ctrl+C` | Sale |

## Paneles

| Panel | Historia | Ruta de Admin |
| --- | --- | --- |
| Simulación | HU2-19 | `GET /carreras` y `POST /simulacion` |
| Resumen financiero | HU2-20 | `POST /consultas` `resumen_financiero` |
| Rentabilidad | HU2-21 | `POST /consultas` `rentabilidad` |
| Horarios | HU2-22 | `POST /consultas` `horarios` |
| Participación | HU2-22 | `POST /consultas` `participacion` |

**Filtros (HU2-23):** evento, desde, hasta, tipo de apuesta, modo, plantilla y top N. La
CLI manda todos los que estén puestos y muestra lo que Admin respondió: cuáles aplicó
(`filtros_aplicados`) y cuáles no aplican a esa consulta (`filtros_no_aplicados`). El id de
un evento se ve en el panel Participación.

## Cómo cumple HU2-17

- **Solo entra el rol admin (RF-21).** `POST /login` de Admin valida contra Usuarios. Con
  rol jugador, Admin responde 403 y la CLI muestra «Solo los administradores pueden usar la
  CLI». Si el token vence (401), la CLI vuelve sola al login.
- **Ninguna opción toca dinero ni apuestas.** La CLI solo consulta, lista carreras y programa
  simulaciones.
- **Panel de simulación.** Muestra las carreras descargadas en MongoDB (las da el
  Ingestor). `Enter` abre un cuadro con los minutos de anticipación (10 por defecto, de 1 a
  120). Los errores de Admin se muestran ahí mismo, por ejemplo el 409 «Ya hay una
  simulación programada o en curso».
- **«Consultando…» sin bloquear (RF-30).** Cada petición corre en una tarea de tokio. El
  panel muestra el indicador y los segundos que lleva, y el administrador puede cambiar de
  panel o lanzar otra consulta mientras tanto. Cada petición lleva un id: si llega una
  respuesta vieja (porque el panel se recargó o cambiaron los filtros), se descarta. Admin
  corta a los 10 s con «La consulta tardó demasiado»; si ni eso llega, la CLI se rinde a
  los 12 s con el mismo aviso.

## Estructura

```
src/main.rs       arranque, terminal y bucle (dibujar → tecla → respuestas)
src/app.rs        estado y teclas; devuelve las peticiones y recibe las respuestas (sin red ni terminal)
src/ejecutor.rs   corre cada petición contra Admin
src/api.rs        cliente REST de Admin y traducción de errores (401, 403, 504, 422…)
src/modelos.rs    respuestas de Admin en JSON
src/ui.rs         dibujo con ratatui
src/formato.rs    miles con punto y horas de Colombia
tests/en_vivo.rs  recorrido contra un Admin real (se salta por defecto)
```

## Pruebas

```bash
cd services/cli
cargo fmt --check && cargo clippy --all-targets && cargo test
```

Las pruebas no necesitan el stack: la lógica de `app.rs` se prueba con teclas y respuestas
falsas, y `ui.rs` se dibuja en una terminal en memoria (`TestBackend`).

Con el stack arriba, `tests/en_vivo.rs` entra con el admin, recorre los cinco paneles e
imprime cada pantalla. También comprueba que un jugador no entra:

```bash
POLEBET_ADMIN_PASSWORD=... POLEBET_JUGADOR_EMAIL=... POLEBET_JUGADOR_PASSWORD=... \
  cargo test --test en_vivo -- --ignored --nocapture
```

Sin Rust instalado, lo mismo con Docker (`target/` queda dentro del contenedor; añade
`--network host` para la prueba en vivo):

```bash
docker run --rm -v "$PWD":/app -w /app -e CARGO_TARGET_DIR=/tmp/target rust:1-slim-bookworm cargo test --locked
```
