# Limpieza del prototipo 1 (P1 → P2)

Inventario de todo lo que quedó del **prototipo 1** y hoy sobra, confunde o
debería retirarse en el **prototipo 2**. La idea es decidir en equipo qué se
borra, qué se archiva como referencia y qué se renombra.

> Convención de numeración: **HU-NN** (un dígito, p. ej. HU-04, HU-06, HU-11)
> son del **P1**; **HU2-NN** son del **P2**.

## Cómo leer esta lista

Cada ítem lleva una etiqueta:

- **BORRAR** — es basura o andamiaje, no lo usa nadie en el P2.
- **ARCHIVAR** — documentación/diagramas del P1, útiles como referencia
  histórica pero no describen el P2. Moverlos a una carpeta aparte o marcarlos.
- **DECIDIR** — está vivo y renderizándose, pero es un flujo heredado del P1
  que el P2 probablemente ya reemplazó. Requiere decisión de producto antes de
  tocarlo.
- **CORREGIR** — texto/comentario desactualizado que menciona el P1.

---

## 1. El caso que motivó este documento: dos barras y tres "mercados"

### 1.1 Barra de navegación duplicada — **CORREGIR** (ya hay issue abierto)

`Mi página` (SSR, HU2-03) trae su **propia** barra hardcodeada
(`services/front/src/ssr/PaginaUsuario.tsx`, clases `pu-*`), separada de la
barra del resto del sitio (`services/front/src/componentes/Layout.tsx`, CSS
Module). Al estar duplicadas quedaron desincronizadas: a la de SSR le falta
"Calendario" y el logo/tipografía difieren. Ya se abrió un issue para
sincronizarlas o extraer una barra compartida.

### 1.2 Tres flujos de "mercado" conviviendo

Hoy coexisten tres caminos distintos que un lector nuevo confunde con facilidad:

| Flujo | Front | Backend (motor) | HU | Estado |
|---|---|---|---|---|
| Mercado manual del admin (Sí/No) | `src/mercados/` + `src/apuestas/` + `paginas/Admin.tsx` | `POST /mercados`, `GET /mercados/actual`, `POST /apuestas` | HU-11 / HU-06 / HU-07 | **P1 heredado, aún vivo** |
| Apuestas dinámicas (automáticas, en vivo) | `src/dinamicas/` | `internal/dinamicas/` | HU2-08/09 | **P2** |
| Mercados estáticos del torneo (antes de largar) | `src/eventos/` (Calendario) | `internal/estaticas/` | HU2-07 | **P2** |

En la pantalla `/carrera`, la columna central renderiza **los tres mundos a la
vez** (`services/front/src/paginas/Carrera.tsx`):

```tsx
<PanelDinamico />     // HU2-08 apuestas dinámicas (P2)
<MercadoJugador />    // HU-11 mercado del admin (P1 heredado)
<PanelResultado />    // HU-08 resultado (P1)
```

El flujo 1 (mercado manual del admin) es el principal candidato a retiro: si el
P2 ya cubre las apuestas con los flujos dinámico (2) y estático (3), el mercado
manual sobra. **Pero hoy está vivo y renderizándose**, no es código muerto que
se pueda borrar sin decisión de producto → **DECIDIR**.

---

## 2. Documentación del P1

Todo esto describe el P1; no aplica al flujo principal del P2. El README ya
trata hu-04 y hu-07 como "referencia del P1".

| Archivo | Qué es | Acción |
|---|---|---|
| `documentation/hu-04-ingesta-openf1.md` | Ingesta OpenF1 del P1 (en P2 es el Ingestor, HU2-16/19) | **ARCHIVAR** |
| `documentation/hu-07-liquidacion-apuestas.md` | Liquidación del mercado Sí/No del P1 (en P2 es HU2-10/11) | **ARCHIVAR** |
| `documentation/HU-11-abrir-cerrar-mercado.md` | Mercado manual del admin (P1); describe código aún vivo | **ARCHIVAR / DECIDIR** (atado a §1.2) |
| `documentation/HU-12-aprovisionamiento-administrador.md` | Aprovisionamiento del admin del P1 | **ARCHIVAR** |
| `documentation/p1_2E.pdf` | Entregable del P1 | **ARCHIVAR** |
| `documentation/modelo-datos-pagos-p1.pdf` / `.png` | Modelo de datos de Pagos del P1 (el vigente es `modelo-datos-p2.*`) | **ARCHIVAR** |

Sugerencia: crear `documentation/p1/` y mover ahí estos archivos, o añadir una
nota al inicio de cada `.md` del P1 aclarando que es histórico.

Vigentes (NO tocar): todos los `hu2-*.md`, `modelo-datos-p2.*`,
`orquestador-api.md`, y los PDFs "… prototipo 2.pdf" de la raíz.

---

## 3. Endpoints / rutas heredadas del P1

### 3.1 `POST /apuestas` en el Motor — **DECIDIR**

- Se registra en `services/motor/main.go` vía `registrarApuestas(...)`,
  comentado como `// HU-06 (P1; la reemplaza HU2-09)`.
- Implementación: `services/motor/internal/apuestas/`.
- La reemplaza `services/motor/internal/apostar/` (HU2-09, `POST
  /internal/apuestas`, que llama el Orquestador). El propio `apostar.go` dice:
  *"Este paquete reemplaza la ruta del P1… Ese paquete sigue vivo porque lo usa
  la liquidación del P1."*
- **Único consumidor entrante:** el flujo P1 del front
  (`src/apuestas/api.js` → `FormularioApuesta.jsx` → `MercadoJugador.tsx`). El
  flujo P2 (dinámicas) NO pasa por aquí: va por el Orquestador.

O sea, `POST /apuestas` + `internal/apuestas/` + el worker `liquidacion` forman
la **cadena de apuesta manual del P1**, paralela a la del P2. Retirarla va de la
mano con la decisión de §1.2.

### 3.2 Otros backends — sin restos de P1

Orquestador, Pagos, Usuarios, Admin e Ingestor solo exponen rutas del P2
(`/internal/*`, auth, HU2-*). No se encontraron rutas P1 colgadas ahí.

---

## 4. Front: componentes heredados del P1

Todos siguen **montados** (no son imports huérfanos), pero pertenecen al flujo
manual del P1:

| Ruta | Qué es | Acción |
|---|---|---|
| `services/front/src/mercados/` (todo el dir) | Mercado manual Sí/No del P1 (`MercadoJugador`, `EstadoMercado`, `PanelResultado`, `useMercado`, …) | **DECIDIR** (§1.2) |
| `services/front/src/apuestas/` (`FormularioApuesta.jsx`, `api.js`) | Formulario de apuesta HU-06 del P1 | **DECIDIR** (§1.2) |
| `services/front/src/paginas/Admin.tsx` | Panel de admin; su propio comentario lo marca como "herramienta PROVISIONAL del prototipo" | **DECIDIR** |

Señal de código viejo: `FormularioApuesta.jsx` es el **único `.jsx`** del front
(el resto es `.tsx`/`.ts`), y `MercadoJugador.tsx` tiene que castearlo con
`as unknown as ComponentType<...>` para consumirlo. Si se decide conservar el
flujo, al menos migrarlo a TypeScript.

---

## 5. Configuración, infra y andamiaje de desarrollo

| Ítem | Ubicación | Acción |
|---|---|---|
| Mock del motor para desarrollar sin Go | `services/front/scripts/mock-ws.mjs` (+ scripts `mock`/`dev:mock` en `package.json`) | **BORRAR** (ya existe el motor real) |
| JWT de dev "mientras no exista el login" | `services/front/scripts/dev-jwt.mjs` (+ script `jwt` en `package.json`) | **BORRAR** (el login ya existe, HU2-01/02) |
| `OPENF1_API_KEY` ausente en `.env` | Está en `.env.example`, falta en `.env` | **CORREGIR** (no es resto de P1, es inconsistencia P2: añadirla al `.env`) |

Nota verificada: `services/front/dist/` y `dist-ssr/` existen localmente pero
**ya están en `.gitignore` (líneas 11-12) y NO están versionados** — no hay nada
que quitar de git. `docker-compose.yml` no tiene servicios del P1. El script
`scripts/migrar-p1.sh` es intencional (migra datos del P1), **no se borra**.

---

## 6. Textos y comentarios desactualizados — **CORREGIR**

| Dónde | Qué dice |
|---|---|
| `services/motor/main.go:1` | Encabezado todavía dice "Motor de apuestas - Prototipo 1 (ArquiSoft)." (el resto del archivo ya es P2) |
| `services/motor/internal/apostar/apostar.go` (~líneas 22-27) | Nota de deuda P1 explícita: el worker de liquidación todavía lee los mercados del P1 en Mongo |
| `services/front/src/paginas/Carrera.tsx` (comentario cabecera) | Describe el centro como "mercado del P1 (HU-11)" — correcto como documentación, se actualiza si se retira el flujo |

---

## 7. Tests heredados del P1

Prueban el flujo manual del P1; se retiran junto con el código si se decide en §1.2:

- `services/front/src/mercados/mercados.test.tsx`, `resultado.test.tsx`
- `services/motor/internal/apuestas/*_test.go`

(`dinamicas.test.tsx` incluso valida que "los mensajes del P1 no lo tocan" — es
del P2 y se queda; confirma la convivencia P1/P2.)

---

## Resumen priorizado

1. **Decisión de producto (bloqueante del resto):** ¿se retira el flujo del
   mercado manual del admin (HU-11)? De eso dependen §1.2, §3.1, §4 y §7.
2. **Borrados seguros ya (sin discusión):** scripts de dev obsoletos
   (`mock-ws.mjs`, `dev-jwt.mjs` + sus scripts en `package.json`).
3. **Archivar docs del P1:** mover `documentation/{hu-04, hu-07, HU-11, HU-12}.md`,
   `p1_2E.pdf`, `modelo-datos-pagos-p1.*` a `documentation/p1/`.
4. **Correcciones de texto rápidas:** encabezado de `motor/main.go`, añadir
   `OPENF1_API_KEY` al `.env`.
5. **Issue ya abierto:** barra de navegación duplicada en "Mi página".
