// Pruebas de la inscripcion al torneo (front de HU2-06, RF-07): la logica
// pura y la tarjeta del detalle del evento con fetch simulado.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorHttp } from "../api/http";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { AuthProvider } from "../auth/AuthContext";
import { USUARIOS_SIMULADOS } from "../auth/usuariosPrueba";
import type { DetalleEvento, Evento } from "./api";
import { bloqueaInscripcion, mensajeErrorInscripcion, textoInscrito, vistaInscripcion } from "./inscripcion";

// ---------------------------------------------------------------------------
// Datos
// ---------------------------------------------------------------------------

const LARGADA = new Date(Date.now() + 3600_000).toISOString();

function evento(cambios: Partial<Evento> = {}): Evento {
  return {
    evento_id: "evt-1",
    carrera: "2024_monza",
    estado: "programado",
    estado_torneo: "abierto",
    largada: LARGADA,
    inscritos: 3,
    botin: 300,
    costo_inscripcion: 100,
    fichas_iniciales: 1000,
    ...cambios,
  };
}

function detalle(cambios: Partial<Evento> = {}, inscrito = false, devuelta = false): DetalleEvento {
  return { evento: evento(cambios), mercados_estaticos: [], mi_inscripcion: { inscrito, devuelta } };
}

// ---------------------------------------------------------------------------
// Logica
// ---------------------------------------------------------------------------

describe("HU2-06 · logica de la inscripcion", () => {
  it.each([
    ["programado, abierto y sin inscribir", detalle(), "inscribible"],
    ["ya inscrito", detalle({}, true), "inscrito"],
    ["inscripcion devuelta", detalle({ estado_torneo: "cancelado" }, true, true), "devuelta"],
    ["cancelado y no inscrito", detalle({ estado_torneo: "cancelado" }), "cancelado"],
    ["en curso", detalle({ estado: "en_curso", estado_torneo: "activo" }), "cerrada"],
    ["programado con la largada ya pasada", detalle({ largada: new Date(Date.now() - 1000).toISOString() }), "cerrada"],
    ["torneo activo (ya largo)", detalle({ estado_torneo: "activo" }), "cerrada"],
  ] as const)("vistaInscripcion: %s", (_, d, vista) => {
    expect(vistaInscripcion(d)).toBe(vista);
  });

  it("textoInscrito: antes de largar, las fichas; ya largado con torneo activo, en curso", () => {
    expect(textoInscrito(evento())).toBe("Inscrito · recibirás 1.000 fichas en la largada");
    expect(textoInscrito(evento({ estado: "en_curso", estado_torneo: "activo" }))).toBe("Inscrito · torneo en curso");
    expect(textoInscrito(evento({ estado: "finalizado", estado_torneo: "activo" }))).toBe("Inscrito · torneo en curso");
    // Programado con torneo abierto (aun no largo): fichas en la largada.
    expect(textoInscrito(evento({ estado_torneo: "abierto" }))).toContain("en la largada");
  });

  it.each([
    [422, "saldo_insuficiente", "No te alcanza el saldo: la inscripción cuesta 1.500 PoleCoins.", false],
    [409, "inscripciones_cerradas", "Las inscripciones ya cerraron", true],
    [404, "evento_no_existe", "El evento ya no existe", true],
    [503, "servicio_no_disponible", "no se cobra dos veces", false],
    [0, undefined, "No se pudo conectar", false],
  ])("error %s %s: mensaje claro", (status, codigo, texto, bloquea) => {
    const e = new ErrorHttp(status, "texto del servidor", codigo);
    expect(mensajeErrorInscripcion(e, 1500)).toContain(texto);
    expect(bloqueaInscripcion(e)).toBe(bloquea);
  });
});

// ---------------------------------------------------------------------------
// Tarjeta en el detalle del evento
// ---------------------------------------------------------------------------

type Respuesta = { status: number; body?: unknown };
let rutas: Record<string, Respuesta | Respuesta[]>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  rutas = {};
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const clave = `${init?.method ?? "GET"} ${url.replace(/^\/api/, "")}`;
    let r = rutas[clave] ?? { status: 404, body: {} };
    if (Array.isArray(r)) r = r.length > 1 ? r.shift()! : r[0];
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body ?? null };
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

const socketMudo = (): SocketLike => ({ onopen: null, onmessage: null, onclose: null, onerror: null, close() {} });

async function abrirDetalle(d: DetalleEvento | Respuesta[]) {
  rutas["GET /eventos"] = { status: 200, body: { eventos: [Array.isArray(d) ? evento() : d.evento] } };
  rutas["GET /eventos/evt-1"] = Array.isArray(d) ? d : { status: 200, body: d };
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
  await screen.findByRole("heading", { name: "2024_monza" });
}

const posts = () => fetchMock.mock.calls.filter(([url, init]) => url === "/api/eventos/evt-1/inscripcion" && init?.method === "POST");
const llamadas = (url: string) => fetchMock.mock.calls.filter(([u, init]) => u === url && (init?.method ?? "GET") === "GET");
const botonInscribirme = () => screen.getByRole("button", { name: "Inscribirme al torneo (100 PoleCoins)" });

const INSCRITO = { evento_id: "evt-1", ya_inscrito: false, costo_inscripcion: 100, fichas_iniciales: 1000, botin: 400, saldo: 900 };

describe("HU2-06 · inscripcion en el detalle del evento", () => {
  it("confirma antes de cobrar; al inscribirse recarga el detalle y el saldo", async () => {
    rutas["GET /billetera"] = [
      { status: 200, body: { saldo: 1000, cop_por_token: 100 } },
      { status: 200, body: { saldo: 900, cop_por_token: 100 } },
    ];
    rutas["POST /eventos/evt-1/inscripcion"] = { status: 201, body: INSCRITO };
    await abrirDetalle([
      { status: 200, body: detalle() },
      { status: 200, body: detalle({ inscritos: 4, botin: 400 }, true) },
    ]);

    fireEvent.click(botonInscribirme());
    expect(posts()).toHaveLength(0); // aun no se cobra
    expect(screen.getByRole("group", { name: "Confirmar inscripción" })).toHaveTextContent(
      "Se descuentan 100 PoleCoins de tu saldo y recibirás 1.000 fichas en la largada.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Sí, inscribirme" }));

    expect(await screen.findByTestId("inscripcion-estado")).toHaveTextContent("Inscrito · recibirás 1.000 fichas en la largada");
    expect(posts()).toHaveLength(1);
    expect(posts()[0][1].body).toBeUndefined(); // sin cuerpo (§3.5)
    expect(screen.getByText(/4 inscritos/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Inscribirme al torneo/ })).not.toBeInTheDocument();
    await waitFor(() => expect(llamadas("/api/billetera")).toHaveLength(2));
    expect(llamadas("/api/eventos/evt-1")).toHaveLength(2);
  });

  it("cancelar la confirmacion no cobra", async () => {
    await abrirDetalle(detalle());
    fireEvent.click(botonInscribirme());
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(botonInscribirme()).toBeInTheDocument();
    expect(posts()).toHaveLength(0);
  });

  it("saldo insuficiente: mensaje claro y se puede reintentar", async () => {
    rutas["POST /eventos/evt-1/inscripcion"] = [
      { status: 422, body: { error: "Saldo insuficiente", codigo: "saldo_insuficiente" } },
      { status: 201, body: INSCRITO },
    ];
    await abrirDetalle([
      { status: 200, body: detalle() },
      { status: 200, body: detalle({}, true) },
    ]);
    fireEvent.click(botonInscribirme());
    fireEvent.click(screen.getByRole("button", { name: "Sí, inscribirme" }));
    expect(await screen.findByTestId("inscripcion-error")).toHaveTextContent("No te alcanza el saldo: la inscripción cuesta 100 PoleCoins.");

    fireEvent.click(screen.getByRole("button", { name: "Sí, inscribirme" }));
    expect(await screen.findByTestId("inscripcion-estado")).toHaveTextContent("Inscrito");
    expect(screen.queryByTestId("inscripcion-error")).not.toBeInTheDocument();
  });

  it("inscripciones cerradas: mensaje y ya no hay boton", async () => {
    rutas["POST /eventos/evt-1/inscripcion"] = { status: 409, body: { error: "Cerradas", codigo: "inscripciones_cerradas" } };
    await abrirDetalle(detalle());
    fireEvent.click(botonInscribirme());
    fireEvent.click(screen.getByRole("button", { name: "Sí, inscribirme" }));
    expect(await screen.findByTestId("inscripcion-error")).toHaveTextContent("Las inscripciones ya cerraron: la carrera largó.");
    expect(screen.queryByRole("button", { name: /Inscribirme al torneo|Sí, inscribirme/ })).not.toBeInTheDocument();
  });

  it("ya inscrito (200 ya_inscrito): avisa que no se cobró de nuevo", async () => {
    rutas["POST /eventos/evt-1/inscripcion"] = { status: 200, body: { ...INSCRITO, ya_inscrito: true } };
    await abrirDetalle([
      { status: 200, body: detalle() },
      { status: 200, body: detalle({}, true) },
    ]);
    fireEvent.click(botonInscribirme());
    fireEvent.click(screen.getByRole("button", { name: "Sí, inscribirme" }));
    expect(await screen.findByText("Ya estabas inscrito en este torneo: no se cobró de nuevo.")).toBeInTheDocument();
    expect(screen.getByTestId("inscripcion-estado")).toHaveTextContent("Inscrito");
  });

  it.each([
    ["inscrito", detalle({}, true), "Inscrito · recibirás 1.000 fichas en la largada"],
    ["inscrito con el torneo en curso", detalle({ estado: "en_curso", estado_torneo: "activo" }, true), "Inscrito · torneo en curso"],
    ["torneo cancelado con la inscripcion devuelta", detalle({ estado_torneo: "cancelado" }, true, true), "Torneo cancelado: te devolvimos la inscripción"],
    ["torneo cancelado sin inscribir", detalle({ estado_torneo: "cancelado" }), "Torneo cancelado"],
  ])("%s: muestra el estado y no el boton", async (_, d, texto) => {
    await abrirDetalle(d);
    expect(screen.getByTestId("inscripcion-estado")).toHaveTextContent(texto);
    expect(screen.queryByRole("button", { name: /Inscribirme al torneo/ })).not.toBeInTheDocument();
  });

  it("si la carrera ya largo no hay boton", async () => {
    await abrirDetalle(detalle({ estado: "en_curso", estado_torneo: "activo" }));
    expect(screen.queryByTestId("inscripcion")).not.toBeInTheDocument();
  });
});
