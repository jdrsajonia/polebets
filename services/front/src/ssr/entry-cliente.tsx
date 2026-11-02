// Entrada del navegador para la pagina SSR (HU2-03): hidratacion.
//
// El servidor ya mando el HTML con los datos y, en window.__PAGINA__, las
// mismas props con las que lo renderizo. hydrateRoot no vuelve a pintar ni a
// pedir nada: solo conecta los eventos (Salir, Actualizar) al HTML existente.

import { StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import PaginaUsuario from "./PaginaUsuario";
import type { PropsPagina } from "./tipos";
import "../estilos/tokens.css";
import "../estilos/base.css";
import "./pagina.css";

declare global {
  interface Window {
    __PAGINA__?: PropsPagina;
  }
}

const raiz = document.getElementById("pagina");
const props = window.__PAGINA__;
if (raiz && props) {
  hydrateRoot(
    raiz,
    <StrictMode>
      <PaginaUsuario {...props} />
    </StrictMode>,
  );
}
