// Entrada del servidor para la pagina SSR (HU2-03). La compila
// `vite build --ssr` y la usa server/servidor.mjs.
//
// Arma el documento HTML completo: el componente ya renderizado, las props en
// window.__PAGINA__ (para que el navegador hidrate con los mismos datos) y los
// assets del build del navegador (JS y CSS de entry-cliente).

import { StrictMode } from "react";
import { renderToString } from "react-dom/server";
import PaginaUsuario from "./PaginaUsuario";
import type { PropsPagina } from "./tipos";

export interface Assets {
  js: string; // /assets/pagina-xxxx.js
  css: string[]; // /assets/pagina-xxxx.css
}

const FUENTES =
  "https://fonts.googleapis.com/css2?family=Barlow:wght@400;500;600;700&family=Barlow+Condensed:ital,wght@0,600;0,700;0,800;1,700;1,800&display=swap";

// JSON seguro dentro de <script>: sin "<" (evita cerrar la etiqueta con
// "</script>" metido en un nombre) ni separadores de linea de JavaScript.
export function jsonSeguro(valor: unknown): string {
  return JSON.stringify(valor)
    .replace(/</g, "\\u003c")
    .replace(new RegExp(String.fromCharCode(0x2028), "g"), "\\u2028")
    .replace(new RegExp(String.fromCharCode(0x2029), "g"), "\\u2029");
}

function escaparAtributo(texto: string): string {
  return texto.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function cabeza(titulo: string, assets: Assets): string {
  const css = assets.css.map((href) => `<link rel="stylesheet" href="${escaparAtributo(href)}" />`).join("");
  return `<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<meta name="theme-color" content="#0B0B0D" />
<title>${titulo}</title>
<link rel="preconnect" href="https://fonts.googleapis.com" />
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
<link href="${FUENTES}" rel="stylesheet" />
${css}
</head>`;
}

// Documento de la pagina del usuario, listo para responder con 200.
export function renderizarPagina(props: PropsPagina, assets: Assets): string {
  const html = renderToString(
    <StrictMode>
      <PaginaUsuario {...props} />
    </StrictMode>,
  );
  return `<!doctype html>
<html lang="es">
${cabeza("Mi página · PoleBet", assets)}
<body>
<div id="pagina">${html}</div>
<script>window.__PAGINA__=${jsonSeguro(props)}</script>
<script type="module" src="${escaparAtributo(assets.js)}"></script>
</body>
</html>`;
}

// Pagina de error sin hidratacion (por ejemplo, Usuarios no responde).
export function renderizarError(mensaje: string, assets: Assets): string {
  const seguro = mensaje.replace(/&/g, "&amp;").replace(/</g, "&lt;");
  return `<!doctype html>
<html lang="es">
${cabeza("PoleBet", assets)}
<body>
<main class="pu-main">
<h1 class="titulo-pagina">Mi página</h1>
<p class="pu-avisos" role="alert">${seguro}</p>
<p><a href="/mi-pagina">Intentar de nuevo</a> · <a href="/carrera">Ir a la carrera</a></p>
</main>
</body>
</html>`;
}
