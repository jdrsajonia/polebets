// Pruebas de la apuesta estatica (front de HU2-09, RF-09/RF-12): la logica
// pura y el formulario del detalle del evento con fetch simulado.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MiApuesta } from "../api/tipos";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { AuthProvider } from "../auth/AuthContext";
import { USUARIOS_SIMULADOS } from "../auth/usuariosPrueba";
import type { DetalleEvento, MercadoEstatico } from "./api";
import { estaAbierto, MAXIMO_INDIVIDUAL, textoAntesDeConfirmar, validarMontoEstatico } from "./apuestaEstatica";

// ---------------------------------------------------------------------------
// Datos
// ---------------------------------------------------------------------------

const LARGADA = new Date(Date.now() + 3600_000).toISOString();

function mercado(cambios: Partial<MercadoEstatico> = {}): MercadoEstatico {
  return {
    mercado_id: "m-1",
    plantilla: "ganador",
    pregunta: "¿Quién gana la carrera?",
    estado: "abierto",
    cierra_en: LARGADA,
    opciones: [
      { opcion: "VER", multiplicador: 2.5 },
      { opcion: "NOR", multiplicador: 3 },
    ],
    ...cambios,
  };
}

function detalle(mercados: MercadoEstatico[] = [mercado()]): DetalleEvento {
  return {
    evento: {
      evento_id: "evt-1", carrera: "2024_monza", estado: "programado", estado_torneo: "abierto", largada: LARGADA,
      inscritos: 3, botin: 300, costo_inscripcion: 100, fichas_iniciales: 1000,
    },
    mercados_estaticos: mercados,
    mi_inscripcion: { inscrito: false, devuelta: false },
  };
}

function miApuesta(cambios: Partial<MiApuesta> = {}): MiApuesta {
  return {
    apuesta_id: "a-1", mercado_id: "m-1", opcion: "VER", monto: 30, modo: "individual", multiplicador: 2.5,
    status: "pending", payout: null, ...cambios,
  };
}

const respuesta = (cambios = {}) => ({
  apuesta_id: "a-1", mercado_id: "m-1", opcion: "VER", monto: 20, modo: "individual", multiplicador: 2.5,
  pago_si_acierta: 50, saldo: 980, fichas: null, ...cambios,
});

// ---------------------------------------------------------------------------
// Logica
// ---------------------------------------------------------------------------

describe("HU2-09 · logica de la apuesta estatica", () => {
  it("validarMontoEstatico: entero > 0 y maximo 500", () => {
    expect(validarMontoEstatico(String(MAXIMO_INDIVIDUAL))).toEqual({ ok: true, monto: 500 });
    const tope = validarMontoEstatico("501");
    expect(tope.ok).toBe(false);
    if (!tope.ok) expect(tope.error).toBe("El máximo por apuesta es 500 PoleCoins");
    for (const malo of ["", "0", "1.5", "abc"]) expect(validarMontoEstatico(malo).ok).toBe(false);
  });

  it("estaAbierto: abierto y antes de cierra_en", () => {
    expect(estaAbierto(mercado())).toBe(true);
    expect(estaAbierto(mercado({ cierra_en: null }))).toBe(true);
    expect(estaAbierto(mercado({ cierra_en: new Date(Date.now() - 1000).toISOString() }))).toBe(false);
    for (const estado of ["programado", "cerrado", "liquidado"] as const) expect(estaAbierto(mercado({ estado }))).toBe(false);
  });

  it("textoAntesDeConfirmar: modo, multiplicador y pago truncado", () => {
    expect(textoAntesDeConfirmar("VER", 2.5, 7)).toBe("Modo individual · «VER» × 2,5: si aciertas recibes 17 PoleCoins.");
    expect(textoAntesDeConfirmar("NOR", 3, 200)).toBe("Modo individual · «NOR» × 3,0: si aciertas recibes 600 PoleCoins.");
  });
});

// ---------------------------------------------------------------------------
// Formulario en el detalle del evento
// ---------------------------------------------------------------------------

type Respuesta = { status: number; body?: unknown };
let rutas: Record<string, Respuesta | Respuesta[]>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  rutas = {};
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const clave = `${init?.method ?? "GET"} ${url.replace(/^\/api/, "")}`;
    let r = rutas[clave] ?? { status: 404, body: { error: "No existe", codigo: "sin_apuesta" } };
    if (Array.isArray(r)) r = r.length > 1 ? r.shift()! : r[0];
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body ?? null };
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

const socketMudo = (): SocketLike => ({ onopen: null, onmessage: null, onclose: null, onerror: null, close() {} });

async function abrirDetalle(d: DetalleEvento = detalle()) {
  rutas["GET /eventos"] = { status: 200, body: { eventos: [d.evento] } };
  rutas["GET /eventos/evt-1"] = { status: 200, body: d };
  render(
    <MemoryRouter initialEntries={["/eventos"]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <AuthProvider usuarioInicial={USUARIOS_SIMULADOS.player}>
        <WsProvider cliente={new ClienteWs({ url: "ws://prueba/ws", crearSocket: socketMudo })}>
          <App />
        </WsProvider>
      </AuthProvider>
    </MemoryRouter>,
  );
  fireEvent.click(await screen.findByText("2024_monza"));
  return screen.findByTestId(`apuesta-estatica-${d.mercados_estaticos[0].mercado_id}`);
}

const apuestas = () => fetchMock.mock.calls.filter(([url, init]) => url === "/api/apuestas" && init?.method === "POST");
const opcion = (panel: HTMLElement, nombre: string) => within(panel).getByRole("button", { name: new RegExp(`^${nombre}`) });

describe("HU2-09 · apuesta estatica en el detalle", () => {
  it("muestra modo, multiplicador y pago ANTES de confirmar; apuesta con la MISMA clave al reintentar", async () => {
    rutas["GET /billetera"] = [
      { status: 200, body: { saldo: 1000, cop_por_token: 100 } },
      { status: 200, body: { saldo: 980, cop_por_token: 100 } },
    ];
    rutas["POST /apuestas"] = [
      { status: 503, body: { error: "Servicio no disponible", codigo: "servicio_no_disponible" } },
      { status: 201, body: respuesta() },
    ];
    const panel = await abrirDetalle();
    const confirmar = await within(panel).findByRole("button", { name: "Confirmar apuesta" });
    expect(within(panel).getByTestId("resumen-estatica")).toHaveTextContent("Modo individual: elige una opción");
    expect(confirmar).toBeDisabled();

    fireEvent.click(opcion(panel, "VER"));
    fireEvent.click(within(panel).getByRole("button", { name: "Sumar 10" }));
    expect(within(panel).getByTestId("resumen-estatica")).toHaveTextContent(
      "Modo individual · «VER» × 2,5: si aciertas recibes 50 PoleCoins.",
    );

    fireEvent.click(confirmar);
    expect(await within(panel).findByTestId("error-estatica")).toHaveTextContent("no se duplica");
    fireEvent.click(confirmar);
    expect(await within(panel).findByTestId("mi-apuesta-estatica")).toHaveTextContent(
      "Apuesta registrada en modo individual: 20 a «VER». Si aciertas recibes 50 PoleCoins. Te quedan 980 PoleCoins.",
    );

    const [[, a], [, b]] = apuestas();
    expect(JSON.parse(a.body)).toEqual({ mercado_id: "m-1", opcion: "VER", monto: 20 });
    expect(a.headers["Idempotency-Key"]).toBeTruthy();
    expect(b.headers["Idempotency-Key"]).toBe(a.headers["Idempotency-Key"]);
    // Ya aposto: sin formulario y con su opcion marcada.
    expect(within(panel).queryByRole("button", { name: "Confirmar apuesta" })).not.toBeInTheDocument();
    expect(opcion(panel, "VER")).toHaveAttribute("aria-pressed", "true");
    expect(opcion(panel, "NOR")).toBeDisabled();
    expect(await screen.findByRole("link", { name: "Saldo: 980 PoleCoins" })).toBeInTheDocument();
  });

  it("monto sobre 500: error y no deja confirmar", async () => {
    const panel = await abrirDetalle();
    fireEvent.click(await within(panel).findByRole("button", { name: /^VER/ }));
    fireEvent.change(within(panel).getByLabelText("Monto"), { target: { value: "600" } });
    expect(within(panel).getByRole("alert")).toHaveTextContent("El máximo por apuesta es 500 PoleCoins");
    expect(within(panel).getByRole("button", { name: "Confirmar apuesta" })).toBeDisabled();
  });

  it("ya_apostaste deshabilita el boton", async () => {
    rutas["POST /apuestas"] = { status: 409, body: { error: "Ya apostaste", codigo: "ya_apostaste" } };
    const panel = await abrirDetalle();
    fireEvent.click(await within(panel).findByRole("button", { name: /^VER/ }));
    fireEvent.click(within(panel).getByRole("button", { name: "Confirmar apuesta" }));
    expect(await within(panel).findByTestId("error-estatica")).toHaveTextContent("Ya apostaste en este mercado.");
    expect(within(panel).getByRole("button", { name: "Confirmar apuesta" })).toBeDisabled();
  });

  it("si ya aposto (mi-apuesta) muestra su apuesta y no deja apostar", async () => {
    rutas["GET /mercados/m-1/mi-apuesta"] = { status: 200, body: miApuesta() };
    const panel = await abrirDetalle();
    expect(await within(panel).findByTestId("mi-apuesta-estatica")).toHaveTextContent("Tu apuesta: 30 PoleCoins a «VER».");
    expect(within(panel).queryByRole("button", { name: "Confirmar apuesta" })).not.toBeInTheDocument();
    expect(opcion(panel, "VER")).toHaveAttribute("aria-pressed", "true");
    expect(opcion(panel, "NOR")).toBeDisabled();
  });

  it("cerrado: sin formulario, 'Cerrado en la largada' y, si aposto, esperando o el resultado", async () => {
    rutas["GET /mercados/m-1/mi-apuesta"] = { status: 200, body: miApuesta() };
    const panel = await abrirDetalle(detalle([mercado({ estado: "cerrado" })]));
    expect(within(panel).getByTestId("estado-estatica")).toHaveTextContent("Cerrado en la largada");
    expect(await within(panel).findByTestId("mi-apuesta-estatica")).toHaveTextContent("Tu apuesta: 30 PoleCoins a «VER». Esperando el resultado…");
    expect(within(panel).queryByRole("button", { name: "Confirmar apuesta" })).not.toBeInTheDocument();
  });

  it("liquidado: muestra el resultado con el payout", async () => {
    rutas["GET /mercados/m-1/mi-apuesta"] = { status: 200, body: miApuesta({ status: "won", payout: 75 }) };
    const panel = await abrirDetalle(detalle([mercado({ estado: "liquidado" })]));
    expect(await within(panel).findByTestId("mi-apuesta-estatica")).toHaveTextContent("¡Ganaste 75 PoleCoins!");
    expect(within(panel).getByTestId("estado-estatica")).toHaveTextContent("Cerrado en la largada");
  });

  it("abierto pero con la largada ya pasada: se trata como cerrado", async () => {
    const panel = await abrirDetalle(detalle([mercado({ cierra_en: new Date(Date.now() - 1000).toISOString() })]));
    expect(within(panel).getByTestId("estado-estatica")).toHaveTextContent("Cerrado en la largada");
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => url === "/api/mercados/m-1/mi-apuesta")).toBe(true));
    expect(within(panel).queryByRole("button", { name: "Confirmar apuesta" })).not.toBeInTheDocument();
  });

  it("dos mercados: cada uno con su campo de monto (ids distintos)", async () => {
    await abrirDetalle(detalle([mercado(), mercado({ mercado_id: "m-2", pregunta: "¿Quién hace la pole?" })]));
    const a = screen.getByTestId("apuesta-estatica-m-1");
    const b = await screen.findByTestId("apuesta-estatica-m-2");
    const montoA = await within(a).findByLabelText("Monto");
    const montoB = await within(b).findByLabelText("Monto");
    expect(montoA.id).toBe("monto-m-1");
    expect(montoB.id).toBe("monto-m-2");
    fireEvent.change(montoA, { target: { value: "50" } });
    expect(montoB).toHaveValue("10");
  });
});
