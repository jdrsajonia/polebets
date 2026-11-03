// Pruebas de HU2-08 en el front: la logica pura (transiciones, cuenta
// regresiva, pago potencial, modo, clave de idempotencia y errores) y el
// panel de /carrera con REST y WebSocket falsos.
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorHttp } from "../api/http";
import type { DetalleEvento, MensajeDinamico } from "../api/tipos";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { AuthProvider } from "../auth/AuthContext";
import { USUARIOS_SIMULADOS } from "../auth/usuariosPrueba";
import { formatoReloj, segundosHasta } from "../mercados/mercado";
import { bloqueaElMercado, claveParaIntento, mensajeErrorApuesta, textoApuesta } from "./apuesta";
import { ajustarMonto, esMensajeDinamico, faseDinamica, pagoSiAcierta, siguienteMercado, validarMonto } from "./dinamico";
import { modoPrevisto, unidad } from "./modo";

// ---------------------------------------------------------------------------
// Datos
// ---------------------------------------------------------------------------

const EVENTO = "11111111-2222-3333-4444-555555555555";

function msg(cambios: Partial<MensajeDinamico> = {}): MensajeDinamico {
  return {
    market_id: "m-1",
    event_id: EVENTO,
    template: "cambio_lider",
    question: "¿Habrá un líder distinto de VER al terminar el próximo minuto?",
    options: [
      { opcion: "Sí", multiplicador: 3.5 },
      { opcion: "No", multiplicador: 1.25 },
    ],
    status: "abierto",
    race_close_s: 130,
    closes_at: new Date(Date.now() + 30_000).toISOString(),
    winning_option: null,
    ...cambios,
  };
}

function detalle(inscrito: boolean, estadoTorneo: string, devuelta = false): DetalleEvento {
  return {
    evento: { evento_id: EVENTO, estado: "en_curso", estado_torneo: estadoTorneo },
    mi_inscripcion: { inscrito, devuelta },
  };
}

// ---------------------------------------------------------------------------
// Logica
// ---------------------------------------------------------------------------

describe("HU2-08 · logica", () => {
  it("esMensajeDinamico acepta el payload del motor y rechaza lo demas", () => {
    expect(esMensajeDinamico(msg())).toBe(true);
    expect(esMensajeDinamico(msg({ winning_option: "No", status: "cerrado" }))).toBe(true);
    expect(esMensajeDinamico(null)).toBe(false);
    expect(esMensajeDinamico({ ...msg(), status: "liquidado" })).toBe(false);
    expect(esMensajeDinamico({ ...msg(), options: [{ opcion: "Sí" }] })).toBe(false);
    expect(esMensajeDinamico({ ...msg(), closes_at: undefined })).toBe(false);
  });

  it("faseDinamica: abierto, cerrado y resuelto", () => {
    expect(faseDinamica(msg())).toBe("abierto");
    expect(faseDinamica(msg({ status: "cerrado" }))).toBe("cerrado");
    expect(faseDinamica(msg({ status: "cerrado", winning_option: "Sí" }))).toBe("resuelto");
  });

  it.each([
    ["sin mercado, uno abierto aparece", null, msg(), "m-1 abierto"],
    ["sin mercado, el cierre de uno desconocido se ignora", null, msg({ status: "cerrado" }), "nada"],
    ["el mismo mercado se cierra", msg(), msg({ status: "cerrado" }), "m-1 cerrado"],
    ["el mismo mercado se resuelve", msg({ status: "cerrado" }), msg({ status: "cerrado", winning_option: "No" }), "m-1 resuelto"],
    ["el cierre de otro mercado se ignora", msg(), msg({ market_id: "m-0", status: "cerrado" }), "m-1 abierto"],
    ["el resultado de otro mercado se ignora", msg(), msg({ market_id: "m-0", status: "cerrado", winning_option: "Sí" }), "m-1 abierto"],
    ["uno nuevo abierto reemplaza al anterior", msg({ status: "cerrado" }), msg({ market_id: "m-2" }), "m-2 abierto"],
    ["un abierto viejo no reabre el cerrado", msg({ status: "cerrado" }), msg(), "m-1 cerrado"],
    ["el mismo abierto repetido lo deja abierto", msg(), msg(), "m-1 abierto"],
    ["un cerrado viejo no borra el resultado", msg({ status: "cerrado", winning_option: "Sí" }), msg({ status: "cerrado" }), "m-1 resuelto"],
  ])("siguienteMercado: %s", (_, actual, llega, esperado) => {
    const r = siguienteMercado(actual, llega);
    expect(r ? `${r.market_id} ${faseDinamica(r)}` : "nada").toBe(esperado);
  });

  it("cuenta regresiva con closes_at (redondea hacia arriba, nunca negativa)", () => {
    const ahora = Date.parse("2026-11-02T20:01:40.800Z");
    expect(segundosHasta("2026-11-02T20:02:10Z", ahora)).toBe(30);
    expect(formatoReloj(segundosHasta("2026-11-02T20:02:10Z", ahora))).toBe("0:30");
    expect(segundosHasta("2026-11-02T20:01:00Z", ahora)).toBe(0);
    expect(segundosHasta("no-es-fecha", ahora)).toBe(0);
  });

  it.each([
    [200, 1.8, 360],
    [10, 3.5, 35],
    [7, 1.35, 9], // 9,45 se trunca como el motor y Pagos
    [3, 1.3, 3], // 3,9: el 1,3 en coma flotante no suma de mas
    [0, 2, 0],
    [1.5, 2, 0],
  ])("pagoSiAcierta(%s, %s) = %s", (monto, mult, pago) => {
    expect(pagoSiAcierta(monto, mult)).toBe(pago);
  });

  it("validarMonto exige un entero mayor que 0; + y - no bajan de 1", () => {
    expect(validarMonto("25")).toEqual({ ok: true, monto: 25 });
    expect(validarMonto(" 25 ")).toEqual({ ok: true, monto: 25 });
    for (const malo of ["", "0", "-5", "1.5", "abc"]) expect(validarMonto(malo).ok).toBe(false);
    expect(ajustarMonto("10", 10)).toBe("20");
    expect(ajustarMonto("10", -10)).toBe("1");
    expect(ajustarMonto("abc", 10)).toBe("10");
  });

  it("modoPrevisto: inscrito con torneo activo = torneo; si no, individual; sin datos, no se sabe", () => {
    expect(modoPrevisto(null)).toBeNull();
    expect(modoPrevisto(detalle(true, "activo"))).toBe("torneo");
    expect(modoPrevisto(detalle(true, "activo", true))).toBe("individual"); // inscripcion devuelta
    expect(modoPrevisto(detalle(true, "cancelado"))).toBe("individual");
    expect(modoPrevisto(detalle(false, "activo"))).toBe("individual");
    expect(unidad("torneo")).toBe("fichas");
    expect(unidad("individual")).toBe("PoleCoins");
    expect(unidad(null)).toBe("PoleCoins o fichas");
  });

  it("claveParaIntento: la misma clave mientras no cambie el intento", () => {
    let n = 0;
    const nueva = () => `clave-${++n}`;
    const intento = { mercado_id: "m-1", opcion: "Sí", monto: 10 };

    const primera = claveParaIntento(null, intento, nueva);
    expect(primera.clave).toBe("clave-1");
    // Reintento tras un error: misma clave.
    expect(claveParaIntento(primera, { ...intento }, nueva).clave).toBe("clave-1");
    // Cambia la opcion, el monto o el mercado: clave nueva.
    expect(claveParaIntento(primera, { ...intento, opcion: "No" }, nueva).clave).toBe("clave-2");
    expect(claveParaIntento(primera, { ...intento, monto: 20 }, nueva).clave).toBe("clave-3");
    expect(claveParaIntento(primera, { ...intento, mercado_id: "m-2" }, nueva).clave).toBe("clave-4");
    // Tras un exito se olvida (previa = null): clave nueva.
    expect(claveParaIntento(null, intento, nueva).clave).toBe("clave-5");
  });

  it.each([
    ["sin resultado", null, "individual", "Apuesta registrada en modo individual: 200 a «Sí». Si aciertas recibes 360 PoleCoins. Te quedan 800 PoleCoins."],
    ["sin resultado, torneo", null, "torneo", "Si aciertas recibes 360 fichas. Te quedan 1140 fichas."],
    ["gano", "Sí", "individual", "¡Acertaste! Recibes 360 PoleCoins."],
    ["perdio", "No", "individual", "Perdiste 200 PoleCoins."],
    ["perdio en torneo", "No", "torneo", "Perdiste 200 fichas."],
  ] as const)("textoApuesta: %s", (_, ganadora, modo, texto) => {
    const a = {
      apuesta_id: "a", mercado_id: "m-1", opcion: "Sí", monto: 200, modo, multiplicador: 1.8,
      pago_si_acierta: 360, saldo: modo === "individual" ? 800 : null, fichas: modo === "torneo" ? 1140 : null,
    };
    const r = textoApuesta(a, ganadora);
    expect(r).toContain(texto);
    if (ganadora === "No") expect(r).not.toMatch(/recib|pago| 0 /i);
  });

  it.each([
    [409, "mercado_cerrado", "El mercado ya cerró", true],
    [409, "ya_apostaste", "Ya apostaste", true],
    [422, "saldo_insuficiente", "saldo de PoleCoins", false],
    [422, "fichas_insuficientes", "fichas del torneo", false],
    [422, "monto_invalido", "Monto no válido", false],
    [503, "servicio_no_disponible", "no se duplica", false],
    [503, undefined, "no se duplica", false],
    [0, undefined, "No se pudo conectar", false],
  ])("error %s %s: mensaje claro", (status, codigo, texto, bloquea) => {
    const e = new ErrorHttp(status, "texto del servidor", codigo);
    expect(mensajeErrorApuesta(e)).toContain(texto);
    expect(bloqueaElMercado(e)).toBe(bloquea);
  });
});

// ---------------------------------------------------------------------------
// Panel en /carrera, con REST y WebSocket falsos
// ---------------------------------------------------------------------------

class SocketFalso implements SocketLike {
  static ultimo: SocketFalso;
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;
  constructor() {
    SocketFalso.ultimo = this;
  }
  close() {}
}

function enviarWs(type: string, payload: unknown) {
  act(() => SocketFalso.ultimo.onmessage?.({ data: JSON.stringify({ type, payload }) }));
}

// Respuestas por "METODO /ruta" (ruta completa, p. ej. /api/apuestas). Una
// lista se consume en orden (la ultima se repite).
type Respuesta = { status: number; body?: unknown };
let rutas: Record<string, Respuesta | Respuesta[]>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  rutas = {};
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const clave = `${init?.method ?? "GET"} ${url}`;
    let r = rutas[clave] ?? { status: 404, body: { error: "No existe", codigo: "no_existe" } };
    if (Array.isArray(r)) r = r.length > 1 ? r.shift()! : r[0];
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body ?? null };
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

function abrirCarrera() {
  const cliente = new ClienteWs({ url: "ws://prueba/ws", crearSocket: () => new SocketFalso(), esperasMs: [10] });
  render(
    <MemoryRouter initialEntries={["/carrera"]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <AuthProvider usuarioInicial={USUARIOS_SIMULADOS.player}>
        <WsProvider cliente={cliente}>
          <App />
        </WsProvider>
      </AuthProvider>
    </MemoryRouter>,
  );
}

const apuestas = () => fetchMock.mock.calls.filter(([url, init]) => url === "/api/apuestas" && init?.method === "POST");
const botonConfirmar = () => screen.getByRole("button", { name: /Confirmar apuesta|Enviando/ });

const respuesta = (cambios = {}) => ({
  apuesta_id: "a-1",
  mercado_id: "m-1",
  opcion: "Sí",
  monto: 10,
  modo: "individual",
  multiplicador: 3.5,
  pago_si_acierta: 35,
  saldo: 990,
  fichas: null,
  ...cambios,
});

describe("HU2-08 · panel de la apuesta dinamica", () => {
  it("sin mensaje no hay mercado; los mensajes del P1 no lo tocan", async () => {
    abrirCarrera();
    expect(await screen.findByText(/aquí aparecerá una apuesta en vivo/)).toBeInTheDocument();
    enviarWs("mercado_abierto", { id: "p1", pregunta: "¿P1?", opciones: ["Sí", "No"], estado: "abierto" });
    expect(screen.queryByTestId("mercado-dinamico")).not.toBeInTheDocument();
  });

  it("muestra la pregunta, Sí/No con su multiplicador, la cuenta y el pago ANTES de confirmar (RF-09)", async () => {
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());

    expect(screen.getByText(msg().question)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sí\s*× 3.5/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /No\s*× 1.25/ })).toBeEnabled();
    expect(screen.getByTestId("cuenta-dinamica")).toHaveTextContent(/Cierra en 0:(30|29)/);
    // Sin la ruta del evento (HU2-05 aun no la publica), el modo se confirma al apostar.
    await waitFor(() => expect(screen.getByTestId("modo-dinamica")).toHaveTextContent("El modo se confirma al apostar"));
    expect(botonConfirmar()).toBeDisabled(); // falta elegir opcion

    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));
    expect(screen.getByTestId("pago-dinamica")).toHaveTextContent("Si aciertas «Sí» recibes 10 × 3.5 = 35 PoleCoins o fichas.");
    fireEvent.click(screen.getByRole("button", { name: "Sumar 10" }));
    expect(screen.getByTestId("pago-dinamica")).toHaveTextContent("20 × 3.5 = 70");
    fireEvent.change(screen.getByLabelText("Monto"), { target: { value: "1.5" } });
    expect(screen.getByRole("alert")).toHaveTextContent("entero");
    expect(botonConfirmar()).toBeDisabled();
  });

  it("con el evento del Orquestador muestra el modo: inscrito con torneo activo = torneo", async () => {
    rutas[`GET /api/eventos/${EVENTO}`] = { status: 200, body: detalle(true, "activo") };
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    await waitFor(() => expect(screen.getByTestId("modo-dinamica")).toHaveTextContent("Modo torneo"));
    fireEvent.click(screen.getByRole("button", { name: /No\s*×/ }));
    expect(screen.getByTestId("pago-dinamica")).toHaveTextContent("10 × 1.25 = 12 fichas.");
  });

  it("apuesta: reintenta con la MISMA Idempotency-Key y muestra la respuesta", async () => {
    rutas["POST /api/apuestas"] = [
      { status: 503, body: { error: "Servicio no disponible", codigo: "servicio_no_disponible" } },
      { status: 201, body: respuesta() },
    ];
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));

    fireEvent.click(botonConfirmar());
    expect(await screen.findByTestId("error-dinamica")).toHaveTextContent("no se duplica");
    fireEvent.click(botonConfirmar());
    expect(await screen.findByTestId("apuesta-dinamica")).toHaveTextContent(
      "Apuesta registrada en modo individual: 10 a «Sí». Si aciertas recibes 35 PoleCoins. Te quedan 990 PoleCoins.",
    );

    const [[, primero], [, segundo]] = apuestas();
    expect(JSON.parse(primero.body)).toEqual({ mercado_id: "m-1", opcion: "Sí", monto: 10 });
    expect(primero.headers["Idempotency-Key"]).toBeTruthy();
    expect(segundo.headers["Idempotency-Key"]).toBe(primero.headers["Idempotency-Key"]);
    // Ya aposto: no hay boton para otra apuesta en este mercado.
    expect(screen.queryByRole("button", { name: /Confirmar apuesta/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sí\s*×/ })).toBeDisabled();
  });

  it("si cambia el monto tras un error, la clave es otra", async () => {
    rutas["POST /api/apuestas"] = { status: 422, body: { error: "Saldo insuficiente", codigo: "saldo_insuficiente" } };
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));
    fireEvent.click(botonConfirmar());
    expect(await screen.findByTestId("error-dinamica")).toHaveTextContent("No te alcanza el saldo de PoleCoins");

    fireEvent.click(screen.getByRole("button", { name: "Restar 10" }));
    fireEvent.click(botonConfirmar());
    await waitFor(() => expect(apuestas()).toHaveLength(2));
    const [[, a], [, b]] = apuestas();
    expect(JSON.parse(b.body).monto).toBe(1);
    expect(b.headers["Idempotency-Key"]).not.toBe(a.headers["Idempotency-Key"]);
  });

  it("ya_apostaste o mercado_cerrado deshabilitan el boton", async () => {
    rutas["POST /api/apuestas"] = { status: 409, body: { error: "Ya apostaste", codigo: "ya_apostaste" } };
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));
    fireEvent.click(botonConfirmar());
    expect(await screen.findByTestId("error-dinamica")).toHaveTextContent("Ya apostaste en este mercado.");
    expect(botonConfirmar()).toBeDisabled();
  });

  it("cerrado: sin boton; resuelto: muestra el ganador y si acerto; ignora el cierre de otro mercado", async () => {
    rutas["POST /api/apuestas"] = { status: 201, body: respuesta() };
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));
    fireEvent.click(botonConfirmar());
    await screen.findByTestId("apuesta-dinamica");

    enviarWs("mercado_dinamico", msg({ market_id: "otro", status: "cerrado" }));
    expect(screen.getByTestId("fase-dinamica")).toHaveTextContent("Abierto");

    enviarWs("mercado_dinamico", msg({ status: "cerrado" }));
    expect(screen.getByTestId("fase-dinamica")).toHaveTextContent("Cerrado · en observación");
    expect(screen.getByTestId("cuenta-dinamica")).toHaveTextContent("Esperando el resultado…");
    expect(screen.queryByRole("button", { name: /Confirmar apuesta/ })).not.toBeInTheDocument();

    enviarWs("mercado_dinamico", msg({ status: "cerrado", winning_option: "No" }));
    expect(screen.getByTestId("fase-dinamica")).toHaveTextContent("Resuelto");
    expect(screen.getByTestId("cuenta-dinamica")).toHaveTextContent("Ganó «No»");
    expect(screen.getByTestId("apuesta-dinamica")).toHaveTextContent("Perdiste 10 PoleCoins.");
    expect(screen.getByTestId("apuesta-dinamica")).not.toHaveTextContent(/recibes|pago/i);
  });

  it("el reenvio del mismo mercado abierto (cada 5 s) no cambia nada", async () => {
    rutas["POST /api/apuestas"] = { status: 201, body: respuesta({ opcion: "No", monto: 30, multiplicador: 1.25, pago_si_acierta: 37 }) };
    abrirCarrera();
    const abierto = msg();
    enviarWs("mercado_dinamico", abierto);
    fireEvent.click(screen.getByRole("button", { name: /No\s*×/ }));
    fireEvent.change(screen.getByLabelText("Monto"), { target: { value: "30" } });
    const pago = screen.getByTestId("pago-dinamica").textContent;

    for (let i = 0; i < 3; i++) enviarWs("mercado_dinamico", abierto);
    expect(screen.getByRole("button", { name: /No\s*×/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Monto")).toHaveValue("30");
    expect(screen.getByTestId("pago-dinamica").textContent).toBe(pago);
    expect(fetchMock.mock.calls.filter(([url]) => url === `/api/eventos/${EVENTO}`)).toHaveLength(1); // el modo no se vuelve a pedir

    fireEvent.click(botonConfirmar());
    const hecha = (await screen.findByTestId("apuesta-dinamica")).textContent;
    for (let i = 0; i < 3; i++) enviarWs("mercado_dinamico", abierto);
    expect(screen.getByTestId("apuesta-dinamica").textContent).toBe(hecha);
    expect(screen.queryByRole("button", { name: /Confirmar apuesta/ })).not.toBeInTheDocument();
    expect(apuestas()).toHaveLength(1);
  });

  it("resultado_apuesta (payload de internal/liquidar) recarga la billetera del header", async () => {
    rutas["GET /api/billetera"] = [
      { status: 200, body: { saldo: 990, cop_por_token: 100 } },
      { status: 200, body: { saldo: 997, cop_por_token: 100 } },
    ];
    abrirCarrera();
    expect(await screen.findByRole("link", { name: "Saldo: 990 PBT" })).toBeInTheDocument();

    // HU2-10: el worker de liquidar avisa al jugador con este payload.
    enviarWs("resultado_apuesta", {
      mercado_id: "m-1", apuesta_id: "a-1", pregunta: "¿?", opcion: "No", opcion_ganadora: "No", gano: true,
      modo: "individual", monto: 10, multiplicador: 1.25, pago: 12, saldo: 997,
    });
    expect(await screen.findByRole("link", { name: "Saldo: 997 PBT" })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([url]) => url === "/api/billetera")).toHaveLength(2);
  });

  it("al abrir otro mercado el formulario arranca limpio", async () => {
    abrirCarrera();
    enviarWs("mercado_dinamico", msg());
    fireEvent.click(screen.getByRole("button", { name: /Sí\s*×/ }));
    enviarWs("mercado_dinamico", msg({ market_id: "m-2", question: "¿Otra?" }));
    expect(screen.getByText("¿Otra?")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sí\s*×/ })).toHaveAttribute("aria-pressed", "false");
  });
});
