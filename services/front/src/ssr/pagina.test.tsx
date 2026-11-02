// Pruebas de HU2-03: pagina del usuario con SSR.
//
// Cada `it` corresponde a un criterio de aceptacion: el HTML del servidor ya
// trae los datos, Pagos caido no rompe la pagina, y el navegador hidrata el
// mismo HTML sin diferencias.
import { act } from "react";
import { createRoot, hydrateRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonSeguro, renderizarError, renderizarPagina } from "./entry-servidor";
import { formatoDelta } from "./formato";
import PaginaUsuario from "./PaginaUsuario";
import type { DatosPagina, PropsPagina } from "./tipos";

const ASSETS = { js: "/assets/pagina-abc.js", css: ["/assets/pagina-abc.css"] };

const DATOS: DatosPagina = {
  usuario: { id: "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f", nombre: "Ana Pérez", email: "ana@correo.co", rol: "player" },
  saldo: 1250,
  eventos: [
    { evento_id: "e1", carrera: "2024_monza", estado: "en_curso", estado_torneo: "activo", largada: "2026-11-02T16:00:00Z",
      devuelta: false, fichas: 1340, puesto: 2, premio: null },
    { evento_id: "e2", carrera: "2024_imola", estado: "finalizado", estado_torneo: "cancelado", largada: "2026-10-10T16:00:00Z",
      devuelta: true, fichas: null, puesto: null, premio: null },
  ],
  apuestas_activas: [
    { apuesta_id: "a1", mercado_id: "m1", evento_id: "e1", opcion: "VER", monto: 200, modo: "individual", multiplicador: 2.5 },
  ],
  transacciones: [
    { tipo: "purchase", tokens: 50, evento_id: null, creada_en: "2026-11-01T15:02:11Z" },
    { tipo: "buy_in", tokens: -100, evento_id: "e1", creada_en: "2026-11-01T15:10:00Z" },
  ],
  avisos: [],
};

const PROPS: PropsPagina = { datos: DATOS, generadaEn: "2026-11-02T03:00:00Z" };

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("HU2-03 · el HTML del servidor ya trae los datos", () => {
  it("perfil, saldo, eventos, apuestas activas y transacciones van en el HTML", () => {
    const html = renderizarPagina(PROPS, ASSETS);

    expect(html).toContain("Ana Pérez");
    expect(html).toContain("ana@correo.co");
    expect(html).toContain("1.250"); // saldo con separador de miles es-CO
    expect(html).toContain("2024_monza");
    expect(html).toContain("1.340"); // fichas en vivo
    expect(html).toContain("Cancelado: inscripción devuelta");
    expect(html).toContain("VER");
    expect(html).toContain("Compra de tokens");
    expect(html).toContain("Inscripción a torneo");
  });

  it("incluye las props para hidratar y los assets del build del navegador", () => {
    const html = renderizarPagina(PROPS, ASSETS);

    expect(html).toMatch(/^<!doctype html>/);
    expect(html).toContain('<div id="pagina">');
    expect(html).toContain("window.__PAGINA__=");
    expect(html).toContain('<script type="module" src="/assets/pagina-abc.js"></script>');
    expect(html).toContain('<link rel="stylesheet" href="/assets/pagina-abc.css" />');
  });

  it("un nombre con </script> no rompe el documento", () => {
    const malicioso = { ...PROPS, datos: { ...DATOS, usuario: { ...DATOS.usuario, nombre: "</script><script>alert(1)</script>" } } };
    const html = renderizarPagina(malicioso, ASSETS);

    expect(html.match(/<\/script>/g)).toHaveLength(2); // solo los dos script del documento
    expect(jsonSeguro("</script>")).toBe('"\\u003c/script>"');
  });
});

describe("HU2-03 · si Pagos no responde, la pagina no se rompe", () => {
  it("muestra el perfil, los avisos y un mensaje en cada seccion que falto", () => {
    const sinPagos: DatosPagina = {
      ...DATOS,
      saldo: null,
      eventos: null,
      apuestas_activas: null,
      transacciones: null,
      avisos: ["No pudimos cargar tu saldo. Intenta de nuevo en un momento."],
    };
    const html = renderizarPagina({ ...PROPS, datos: sinPagos }, ASSETS);

    expect(html).toContain("Ana Pérez");
    expect(html).toContain('role="alert"');
    expect(html).toContain("No pudimos cargar tu saldo.");
    expect(html).toContain("No pudimos cargar tus eventos.");
    expect(html).toContain("No pudimos cargar tus apuestas activas.");
    expect(html).toContain("No pudimos cargar tus últimas transacciones.");
  });

  it("sin actividad muestra los estados vacios", () => {
    const vacio: DatosPagina = { ...DATOS, eventos: [], apuestas_activas: [], transacciones: [] };
    const html = renderizarPagina({ ...PROPS, datos: vacio }, ASSETS);

    expect(html).toContain("Aún no te inscribes a ningún torneo.");
    expect(html).toContain("No tienes apuestas pendientes.");
    expect(html).toContain("Todavía no tienes movimientos.");
  });

  it("la pagina de error escapa el mensaje", () => {
    expect(renderizarError("<b>x</b>", ASSETS)).toContain("&lt;b>x&lt;/b>");
  });
});

describe("HU2-03 · hidratacion", () => {
  it("el navegador adopta el HTML del servidor sin diferencias", async () => {
    const html = renderizarPagina(PROPS, ASSETS);
    const contenido = html.slice(html.indexOf('<div id="pagina">') + '<div id="pagina">'.length, html.indexOf("</div>\n<script>"));
    document.body.innerHTML = `<div id="pagina">${contenido}</div>`;
    const errores = vi.spyOn(console, "error").mockImplementation(() => {});

    await act(async () => {
      hydrateRoot(document.getElementById("pagina")!, <PaginaUsuario {...PROPS} />);
    });

    expect(errores).not.toHaveBeenCalled(); // React avisa por console.error si no coinciden
    expect(document.querySelector("button")).toHaveTextContent("Salir");
  });

  it("Salir cierra la sesion en Usuarios y vuelve al ingreso", async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    document.body.innerHTML = '<div id="pagina"></div>';

    await act(async () => {
      createRoot(document.getElementById("pagina")!).render(<PaginaUsuario {...PROPS} />);
    });
    await act(async () => {
      document.querySelector<HTMLButtonElement>(".pu-salir")!.click();
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", { method: "POST", credentials: "include" });
    expect(assign).toHaveBeenCalledWith("/login");
    vi.unstubAllGlobals();
  });
});

describe("formato", () => {
  it("los movimientos llevan signo", () => {
    expect(formatoDelta(50)).toBe("+50");
    expect(formatoDelta(-1200)).toBe("−1.200");
    expect(formatoDelta(0)).toBe("0");
  });
});
