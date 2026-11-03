// Servidor del Front (HU2-03): Node, sin dependencias extra.
//
//   GET /mi-pagina   pagina del usuario renderizada en el SERVIDOR (SSR):
//                    pide los datos al API Gateway reenviando la cookie del
//                    jugador (GET /api/pagina-usuario -> Orquestador) y responde
//                    el HTML ya armado, que el navegador luego hidrata.
//   GET /health      healthcheck del contenedor.
//   resto            la SPA (dist/): archivos estaticos y, para rutas de React
//                    Router (/carrera, /login...), index.html.
//   /_framework/, /_content/
//                    el visor de pista (HU2-15, Blazor WebAssembly) que el
//                    Dockerfile copia a dist/. Si el navegador acepta Brotli
//                    se manda la version .br que ya genera dotnet publish.
//
// Variables de entorno:
//   PORT         puerto de escucha (3000, como en la vista de despliegue)
//   GATEWAY_URL  donde esta el Gateway desde este contenedor (http://gateway:80)

import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, normalize, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const RAIZ = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DIST = join(RAIZ, "dist");
const PORT = Number(process.env.PORT ?? 3000);
const GATEWAY_URL = (process.env.GATEWAY_URL ?? "http://gateway:80").replace(/\/$/, "");
const TIMEOUT_MS = Number(process.env.SSR_TIMEOUT_MS ?? 8000);

const { renderizarPagina, renderizarError } = await import(join(RAIZ, "dist-ssr", "entry-servidor.js"));

// JS y CSS del build del navegador para la entrada de la pagina SSR.
async function leerAssets() {
  const manifiesto = JSON.parse(await readFile(join(DIST, ".vite", "manifest.json"), "utf8"));
  const entrada = manifiesto["src/ssr/entry-cliente.tsx"];
  if (!entrada) throw new Error("el manifest no tiene src/ssr/entry-cliente.tsx");
  // El CSS compartido con la SPA (tokens.css, base.css) queda en un chunk
  // aparte que importa la entrada: hay que recorrer sus imports tambien.
  const css = new Set();
  const visitados = new Set();
  const recorrer = (clave) => {
    if (visitados.has(clave) || !manifiesto[clave]) return;
    visitados.add(clave);
    for (const c of manifiesto[clave].imports ?? []) recorrer(c);
    for (const c of manifiesto[clave].css ?? []) css.add(`/${c}`);
  };
  recorrer("src/ssr/entry-cliente.tsx");
  return { js: `/${entrada.file}`, css: [...css] };
}
const ASSETS = await leerAssets();

const TIPOS = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
  ".txt": "text/plain; charset=utf-8",
  // Visor de pista (Blazor): sin application/wasm el navegador no puede
  // compilar el .wasm mientras lo descarga.
  ".wasm": "application/wasm",
  ".dat": "application/octet-stream",
  ".mjs": "text/javascript; charset=utf-8",
};

// Carpetas de archivos que nunca caen a index.html: si no existe, 404.
const SIN_CAIDA_A_SPA = ["/assets/", "/_framework/", "/_content/"];
// Carpetas del visor que traen su version .br precomprimida.
const CON_BROTLI = ["/_framework/", "/_content/"];

function responderHtml(res, estado, html) {
  res.writeHead(estado, { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" });
  res.end(html);
}

// ---------------------------------------------------------------------------
// SSR: /mi-pagina
// ---------------------------------------------------------------------------
async function paginaUsuario(req, res) {
  let resp;
  try {
    resp = await fetch(`${GATEWAY_URL}/api/pagina-usuario`, {
      // Solo se reenvia la cookie: el Gateway la valida con auth_request y le
      // pone X-User-Id / X-User-Rol al Orquestador. El servidor SSR no lee el JWT.
      headers: { cookie: req.headers.cookie ?? "", accept: "application/json" },
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
  } catch (error) {
    console.error("SSR: el Gateway no respondio:", error.message);
    return responderHtml(res, 503, renderizarError("No pudimos cargar tu página. Intenta de nuevo en un momento.", ASSETS));
  }

  if (resp.status === 401) {
    // Sin sesion (o vencida): al ingreso, y despues de vuelta aqui.
    res.writeHead(302, { Location: "/login", "Cache-Control": "no-store" });
    return res.end();
  }
  if (resp.status === 403) {
    // El admin no tiene pagina de jugador: usa la CLI.
    return responderHtml(res, 403, renderizarError("Esta página es solo para jugadores.", ASSETS));
  }
  if (!resp.ok) {
    console.error(`SSR: /api/pagina-usuario respondio ${resp.status}`);
    return responderHtml(res, 503, renderizarError("No pudimos cargar tu página. Intenta de nuevo en un momento.", ASSETS));
  }

  const datos = await resp.json();
  return responderHtml(res, 200, renderizarPagina({ datos, generadaEn: new Date().toISOString() }, ASSETS));
}

// ---------------------------------------------------------------------------
// SPA: archivos de dist/ con caida a index.html
// ---------------------------------------------------------------------------
async function estatico(req, res) {
  const ruta = decodeURIComponent(new URL(req.url, "http://x").pathname);
  const archivo = normalize(join(DIST, ruta));
  // Nunca servir algo fuera de dist/ (p. ej. /../server/servidor.mjs).
  if (!archivo.startsWith(DIST + sep) && archivo !== DIST) {
    res.writeHead(404);
    return res.end();
  }
  try {
    const info = await stat(archivo);
    if (info.isFile()) {
      const cache = ruta.startsWith("/assets/") ? "public, max-age=31536000, immutable" : "no-cache";
      const cabeceras = { "Content-Type": TIPOS[extname(archivo)] ?? "application/octet-stream", "Cache-Control": cache };
      const comprimido = await versionBrotli(req, ruta, archivo);
      if (comprimido) {
        res.writeHead(200, { ...cabeceras, "Content-Encoding": "br", Vary: "Accept-Encoding" });
        return createReadStream(comprimido).pipe(res);
      }
      res.writeHead(200, cabeceras);
      return createReadStream(archivo).pipe(res);
    }
  } catch {
    /* no existe: es una ruta de la SPA */
  }
  if (SIN_CAIDA_A_SPA.some((prefijo) => ruta.startsWith(prefijo))) {
    res.writeHead(404);
    return res.end();
  }
  res.writeHead(200, { "Content-Type": TIPOS[".html"], "Cache-Control": "no-cache" });
  return createReadStream(join(DIST, "index.html")).pipe(res);
}

// Ruta del .br del archivo si es del visor, existe y el navegador acepta
// Brotli; si no, null. Los ~6 MB del runtime de Blazor bajan a ~2 MB.
async function versionBrotli(req, ruta, archivo) {
  if (!CON_BROTLI.some((prefijo) => ruta.startsWith(prefijo))) return null;
  if (!/\bbr\b/.test(req.headers["accept-encoding"] ?? "")) return null;
  try {
    return (await stat(`${archivo}.br`)).isFile() ? `${archivo}.br` : null;
  } catch {
    return null;
  }
}

const servidor = createServer(async (req, res) => {
  try {
    const ruta = new URL(req.url, "http://x").pathname;
    if (req.method !== "GET" && req.method !== "HEAD") {
      res.writeHead(405, { Allow: "GET, HEAD" });
      return res.end();
    }
    if (ruta === "/health") {
      res.writeHead(200, { "Content-Type": "application/json" });
      return res.end('{"estado":"ok","servicio":"front"}');
    }
    if (ruta === "/mi-pagina" || ruta === "/mi-pagina/") return await paginaUsuario(req, res);
    return await estatico(req, res);
  } catch (error) {
    console.error("front: error inesperado:", error);
    if (!res.headersSent) responderHtml(res, 500, renderizarError("Ocurrió un error inesperado.", ASSETS));
    else res.end();
  }
});

servidor.listen(PORT, () => console.log(`front (SPA + SSR) escuchando en :${PORT}, gateway ${GATEWAY_URL}`));

for (const senal of ["SIGINT", "SIGTERM"]) {
  process.on(senal, () => servidor.close(() => process.exit(0)));
}
