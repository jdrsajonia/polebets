# Front de PoleBet (React + Vite + TypeScript)

Capa de presentación. No sabe de dónde vienen los datos: solo habla con Pagos y con el motor por REST, y con el motor por WebSocket.

## Estructura

```
index.html
vite.config.ts            # proxy de desarrollo y configuración de Vitest
scripts/mock-ws.mjs       # motor de mentira para desarrollar sin el motor de Go
scripts/dev-jwt.mjs       # firma un JWT de desarrollo (pruebas manuales contra el motor)
src/
  main.tsx                # proveedores: router, sesión y WebSocket
  App.tsx                 # rutas
  estilos/tokens.css      # paleta, tipografía y medidas (variables CSS)
  estilos/base.css        # clases compartidas: .tarjeta, .boton, .etiqueta...
  api/config.ts           # URLs de los backends
  api/http.ts             # wrapper de fetch (cookie, 401, 403, errores)
  api/ws.ts               # cliente WebSocket con reconexión
  api/WsContexto.tsx      # una sola conexión para toda la app: useWs()
  api/tipos.ts            # tipos de mensajes y respuestas
  auth/AuthContext.tsx    # sesión: useAuth() (HU-02, login real contra Pagos)
  auth/api.ts             # llamadas /auth/login, /auth/logout, /auth/me
  auth/destino.ts         # a dónde ir después del login según el rol
  auth/RutaProtegida.tsx  # protege rutas por sesión y rol
  componentes/            # Layout (header) y Pendiente (placeholder)
  paginas/                # una pantalla por ruta
  posiciones/             # HU-05: posiciones en vivo
  mercados/               # HU-11: mercado actual, panel del admin y estado en /carrera
  apuestas/               # HU-06: apostar (JavaScript)
  dinamicas/              # HU2-08: apuesta dinámica en vivo en /carrera
  visor/                  # HU2-15: tarjeta del visor de pista (<visor-pista>, Blazor) y "Siguiendo"
```

## Rutas

| Ruta | Pantalla | Historia | Quién entra |
| --- | --- | --- | --- |
| `/registro` | Registro | HU-01 | todos |
| `/login` | Inicio de sesión | HU-02 | todos |
| `/carrera` | Posiciones en vivo y mercado | HU-05, HU-06, HU-08 | jugador |
| `/billetera` | Saldo y compra de tokens | HU-03 | jugador |
| `/admin` | Abrir mercados | HU-11, HU-12 | admin |
| `/estado` | Diagnóstico de backends y WebSocket | HU-10 | todos |

Las pantallas sin construir muestran «Pendiente: HU-XX». Para completar una, reemplaza el contenido de su archivo en `src/paginas/`; el header y el estilo ya vienen del `Layout`. Si agregas una pantalla nueva, regístrala en `App.tsx` y, si va en el menú, en `ENLACES` de `componentes/Layout.tsx`.

## Correr

Necesitas Node 20 o superior.

```bash
cd services/front
npm install
npm run dev        # http://localhost:5173, contra los backends reales
```

Sin Node instalado, lo mismo con Docker:

```bash
docker run --rm -it --network host -u $(id -u):$(id -g) -e HOME=/tmp \
  -v "$PWD":/app -w /app node:22-alpine sh -c "npm install && npm run dev"
```

| Comando | Qué hace |
| --- | --- |
| `npm run dev` | Servidor de desarrollo contra los backends reales |
| `npm run dev:mock` | Igual, pero el motor es el mock |
| `npm run mock` | Levanta el mock del motor en el puerto 8090 |
| `npm run jwt` | Firma un JWT de desarrollo |
| `npm test` | Vitest (`*.test.ts(x)`), incluye la prueba de HU-06 |
| `npm run build` | Revisa los tipos y compila a `dist/` |

## Cómo se conecta con los backends

En desarrollo el front llama a `/api/pagos/...` y `/api/motor/...` en su mismo origen, y el proxy de Vite reenvía a `localhost:8000` y `localhost:8080` quitando el prefijo. Así la cookie de sesión viaja sola, también al abrir el WebSocket.

En Docker no hay proxy: `docker-compose.yml` inyecta las URLs absolutas (`VITE_PAGOS_URL`, `VITE_MOTOR_URL`, `VITE_MOTOR_WS_URL`) al construir la imagen.

```ts
// Las rutas son de ejemplo.
import { http } from "../api/http";

const saldo = await http.get<Saldo>("pagos", "/billetera");          // GET  {pagos}/billetera
const compra = await http.post<Compra>("pagos", "/compras", { cop }); // POST con cuerpo JSON
```

`http` siempre envía la cookie y, si algo falla, lanza un `ErrorHttp` con `status` y `message` en español (`status` 0 = no hubo respuesta). Un 401 cierra la sesión, lo que lleva a `/login`; un 403 muestra un aviso de acceso denegado.

## Variables de entorno

Opcionales. Copia `.env.example` a `.env` dentro de `services/front/` para cambiarlas.

| Variable | Por defecto | Para qué |
| --- | --- | --- |
| `VITE_PAGOS_URL` | `/api/pagos` (proxy) | URL de Pagos |
| `VITE_MOTOR_URL` | `/api/motor` (proxy) | URL REST del motor |
| `VITE_MOTOR_WS_URL` | mismo origen | URL WebSocket del motor, sin `/ws` |
| `VITE_SHOW_LATENCY` | `false` | `true` muestra la latencia en `/carrera` |

Solo para el servidor de desarrollo: `PROXY_MOTOR` y `PROXY_PAGOS` cambian a dónde reenvía el proxy.

## Sesión (HU-02)

El login es real contra Pagos. El JWT vive en una cookie `HttpOnly` que JavaScript **no puede leer**; el navegador la envía sola en cada petición (`credentials: "include"`) a Pagos y al motor, también al abrir el WebSocket.

| Momento | Qué pasa |
| --- | --- |
| Carga la app | `GET /auth/me`. Mientras responde, `cargando` es `true` y las rutas protegidas esperan (recargar no saca al usuario) |
| `/login` | `iniciarSesion(email, password)` → `POST /auth/login`. Un 401 muestra «Correo o contraseña incorrectos» |
| Botón Salir | `cerrarSesion()` → `POST /auth/logout` y vuelve a `/login` (limpia el front aunque Pagos no responda) |
| Cualquier 401 | `http.ts` avisa y la sesión se limpia; `RutaProtegida` lleva a `/login` |

```tsx
const { usuario, cargando, iniciarSesion, cerrarSesion } = useAuth();
// usuario: { id, nombre, email, rol: "player" | "admin" } | null
```

Para probar a mano hace falta un usuario en la BD. Mientras no existan el registro (HU-01) ni el admin automático (HU-12), créalo con la herramienta de desarrollo de Pagos:

```bash
docker compose exec pagos python -m app.cli crear-usuario --email ana@arqui.local --nombre "Ana Pérez" --password clave123
```

**Desarrollo con el motor simulado** (`npm run dev:mock`): el proxy de Vite apunta al mock de `scripts/mock-ws.mjs` en vez del motor de Go, así se desarrolla la carrera en vivo sin levantar el motor. El login sigue siendo real contra Pagos. El motor simulado no valida la sesión, pero el motor real sí rechaza un WebSocket sin cookie; para probar contra el motor real, firma un token:

```bash
npm run jwt                    # jugador, 120 minutos
npm run jwt -- --rol admin
npm run jwt -- --min 1         # vence en un minuto
```

El script imprime el token y la línea para pegarlo como cookie `session` en la consola del navegador. Toma el secreto de `JWT_SECRET` o del `.env` de la raíz del repo. Es solo para desarrollo.

## WebSocket

Hay una sola conexión para toda la app. Se abre al iniciar sesión y, si se cae, se reconecta sola con espera de 1, 2, 4, 8 y luego 10 segundos.

```tsx
const { estado, suscribir } = useWs(); // estado: conectando | abierta | reconectando | cerrada

useEffect(() => suscribir<MiPayload>(TIPOS_WS.mercadoAbierto, (payload) => { /* ... */ }), [suscribir]);
```

`suscribir` devuelve la función para desuscribirse, por eso se puede retornar directo desde el `useEffect`. Los mensajes llegan con el sobre del motor, `{ "type": "...", "payload": { ... } }`, y los tipos están en `TIPOS_WS` (`api/tipos.ts`), con los mismos nombres de `services/motor/internal/ws/mensajes.go`.

## HU-05: posiciones en vivo

`/carrera` pide primero `GET /posiciones` al motor y luego escucha el mensaje `posiciones`. Cuando el WebSocket vuelve de una caída, pide la foto otra vez. Mientras no hay conexión se ve el aviso «Reconectando…», y la fila de un piloto que cambia de puesto se resalta un momento.

Formato de la foto (respuesta del `GET` y `payload` del mensaje). **Es una propuesta**: el esquema final depende de HU-04; cuando se cierre se ajusta en `api/tipos.ts` y en el mock.

```json
{
  "carrera_id": "2026_costa_azul",
  "vuelta": 18,
  "actualizado_en": "2026-10-04T18:30:00.000Z",
  "emitido_en": "2026-10-04T18:30:00.120Z",
  "pilotos": [
    { "posicion": 1, "numero": 4, "codigo": "VAR", "nombre": "L. Varga", "equipo": "Scuderia Roja", "color_equipo": "#E10600", "diferencia": "Líder" }
  ]
}
```

`emitido_en` es el momento en que el motor envía el mensaje; con `VITE_SHOW_LATENCY=true` la pantalla muestra cuánto tardó en llegar (RNF-04). `vuelta`, `color_equipo` y `emitido_en` son opcionales.

Falta en el motor: el módulo `posiciones` (`GET /posiciones` y la difusión del mensaje). Hasta entonces, HU-05 se prueba con el mock.

### Mock del motor

```bash
npm run mock        # terminal 1: motor falso en http://localhost:8090
npm run dev:mock    # terminal 2: Vite con el proxy del motor apuntando al mock
```

El mock responde `GET /posiciones` y `GET /health`, y emite `posiciones` cada 2 segundos con un adelantamiento al azar. No valida la sesión. Para ver la reconexión, detén el mock con Ctrl+C y vuelve a levantarlo. `MOCK_PORT` y `MOCK_INTERVALO_MS` cambian el puerto y el ritmo.

#### Sin Node instalado (solo Docker)

Desde `services/front`, en una terminal de Linux, macOS o WSL. Primero el front, que también instala las dependencias:

```bash
# Terminal 1: front en modo mock -> http://localhost:5173
docker run --rm -it --name polebet-front -p 5173:5173 \
  -u $(id -u):$(id -g) -e HOME=/tmp -e VITE_SHOW_LATENCY=true \
  -v "$PWD":/app -w /app node:22-alpine sh -c "npm install && npm run dev:mock"
```

Cuando Vite diga que está listo, levanta el mock en otra terminal. Comparte la red del contenedor del front, así el proxy lo encuentra en `localhost:8090`:

```bash
# Terminal 2: mock del motor
docker run --rm -it --name polebet-mock --network container:polebet-front \
  -u $(id -u):$(id -g) -e HOME=/tmp \
  -v "$PWD":/app -w /app node:22-alpine npm run mock
```

Abre http://localhost:5173. Para ver la reconexión, detén la terminal 2 con Ctrl+C y vuelve a correr su comando. Al terminar, detén primero el mock y luego el front.

## Apuestas dinámicas (HU2-08)

En `/carrera`, el panel «Apuesta en vivo» (`dinamicas/PanelDinamico.tsx`)
escucha el mensaje `mercado_dinamico` del motor (`useMercadoDinamico`). Convive
con el mercado del P1 (`mercado_abierto/cerrado/liquidado`), que no cambia.

- **Fases:** abierto (se apuesta), cerrado (observación) y resuelto
  (`winning_option`). El cierre de un mercado que no se vio abierto se ignora.
- **Quien entra con un mercado ya abierto:** no hay ruta REST del mercado
  dinámico actual, pero el motor reenvía el abierto cada 5 s; quien entra o
  recarga la página lo ve en a lo sumo 5 s. Recibir el mismo mercado abierto
  varias veces no cambia nada (opción elegida, monto ni apuesta hecha).
- **Cuenta regresiva** con `closes_at`: solo informativa; el motor cierra con el
  tiempo de carrera (RNF-19) y rechaza lo que llegue tarde.
- **Antes de confirmar (RF-09):** «si aciertas recibes monto × multiplicador»
  (truncado como el motor y Pagos) y el modo. El modo sale de
  `GET /api/eventos/{event_id}` (inscrito con torneo `activo` = torneo, si no
  individual; `documentation/orquestador-api.md` §3.4). Si esa ruta aún no
  existe (HU2-05), dice «el modo se confirma al apostar»: el punto de conexión
  es `dinamicas/api.ts` (`obtenerEvento`) y no hay que cambiar nada más.
- **Apostar:** `POST /api/apuestas {mercado_id, opcion, monto}` (§3.6) con una
  `Idempotency-Key` por intento (`dinamicas/apuesta.ts`): los reintentos la
  reutilizan y solo se renueva tras un éxito o si cambian mercado, opción o
  monto. Los errores del contrato se leen por `codigo` (`ErrorHttp.codigo`).

## Visor de pista (HU2-15)

El visor es un componente en C# / Blazor WebAssembly (`services/visor`) que se usa como
el elemento HTML `<visor-pista>`. React no le pasa datos: el visor abre su propio
WebSocket a `/api/ws` y dibuja solo. `/carrera` le dice a quién seguir:

```tsx
<visor-pista seguir="44" mostrar-codigos="true" />
```

| Archivo | Qué hace |
| --- | --- |
| `visor/cargarVisor.ts` | Carga `/_framework/blazor.webassembly.js` una sola vez y solo al abrir `/carrera`; espera a que exista `<visor-pista>` |
| `visor/VisorPista.tsx` | Tarjeta del mockup 07: el elemento, el botón «Mostrar códigos» y el aviso si el visor no carga |
| `visor/PanelSiguiendo.tsx` | Tarjeta «Siguiendo» con el piloto resaltado |
| `posiciones/TablaPosiciones.tsx` | Con `alSeguir`, cada piloto es un botón que lo sigue en el visor |

Mientras el jugador no elige, el visor sigue al líder.

**Dónde están los archivos.** El Dockerfile publica el visor con el SDK de .NET (contexto
adicional `visor` del compose) y copia `_framework/` y `_content/` a `dist/`.
`server/servidor.mjs` los sirve con `application/wasm`, responde 404 (no `index.html`)
si falta uno y manda la versión `.br` si el navegador acepta Brotli. Por eso la imagen
del Front se construye con `docker compose build front`; un `docker build` solo de
`services/front` no tiene el código del visor. Con `npm run dev` no hay `_framework/` y
la tarjeta muestra «Visor no disponible».

## Página del usuario con SSR (HU2-03)

`/mi-pagina` no la pinta React en el navegador: la arma el **servidor Node**
del Front (`server/servidor.mjs`), que es lo que corre en el contenedor
`front` (puerto interno 3000, vista de despliegue).

1. El navegador pide `GET /mi-pagina` (navegación completa: el enlace «Mi
   página» del header es un `<a>`, no un `NavLink`).
2. El servidor Node pide `GET {GATEWAY_URL}/api/pagina-usuario` reenviando la
   cookie del jugador. El Gateway la valida con `auth_request` y el
   Orquestador junta Usuarios, Pagos y Motor en una sola respuesta.
3. `src/ssr/entry-servidor.tsx` renderiza `PaginaUsuario` con
   `renderToString` y mete las mismas props en `window.__PAGINA__`.
   «Ver código fuente» ya muestra perfil, saldo, eventos, apuestas activas y
   las últimas 10 transacciones.
4. En el navegador, `src/ssr/entry-cliente.tsx` hace `hydrateRoot`: conecta
   los botones (Salir, Actualizar) al HTML que ya llegó, sin volver a pedir datos.

Sin sesión, `/mi-pagina` redirige a `/login`. Si Pagos no responde, la página
sale con el perfil y un aviso en cada sección que faltó. Las fechas usan zona
`America/Bogota` y locale `es-CO` fijos para que el HTML del servidor y el del
navegador coincidan (si no, React avisa que la hidratación no coincide).

`npm run build` hace dos builds: el del navegador (`dist/`, con la SPA y la
entrada de hidratación, más `dist/.vite/manifest.json`) y el del servidor
(`dist-ssr/`). Para probarlo sin Docker, con el resto del sistema arriba:

```bash
npm run build
GATEWAY_URL=http://localhost PORT=3000 npm start
```

El resto de rutas las sirve el mismo servidor desde `dist/`, con caída a
`index.html` para React Router.

## Estilo

Los colores, fuentes y medidas salen de los mockups del equipo y están en `estilos/tokens.css`. Usa esas variables (`var(--accent)`, `var(--surface)`...) y las clases de `estilos/base.css` en vez de valores sueltos. Para estilos propios de un componente, usa un CSS Module (`MiComponente.module.css`). Los botones y enlaces táctiles miden al menos 44 px de alto (`var(--tap)`).
