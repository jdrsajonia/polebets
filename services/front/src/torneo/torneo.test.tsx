// Pruebas del ranking del torneo en /carrera (RF-18, front de HU2-11): la
// logica pura y el panel con REST y WebSocket falsos.
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DetalleEvento, FilaRanking, RankingTorneo } from "../api/tipos";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { AuthProvider } from "../auth/AuthContext";
import { USUARIOS_SIMULADOS } from "../auth/usuariosPrueba";
import { anchoBarra, esRankingTorneo, etiquetas, mostrarRanking, siguienteRanking } from "./ranking";

const EVENTO = "11111111-2222-3333-4444-555555555555";
const YO = USUARIOS_SIMULADOS.player.id;
const OTRO_A = "aaaaaaaa-0000-4000-8000-000000000000";
const OTRO_B = "bbbbbbbb-0000-4000-8000-000000000000";

function fila(usuario_id: string, cambios: Partial<FilaRanking> = {}): FilaRanking {
  return { puesto: 1, usuario_id, fichas: 1000, apostado: 0, avance_pct: 0, elegible: false, ...cambios };
}

function ranking(cambios: Partial<RankingTorneo> = {}): RankingTorneo {
  return {
    evento_id: EVENTO,
    filas: [
      fila(OTRO_B, { puesto: 1, fichas: 1350, apostado: 600, avance_pct: 100, elegible: true }),
      fila(YO, { puesto: 2, fichas: 1100, apostado: 250, avance_pct: 50 }),
      fila(OTRO_A, { puesto: 3, fichas: 700, apostado: 300, avance_pct: 60 }),
    ],
    min_apostado: 500,
    botin: 1200,
    actualizado_en: "2026-11-03T20:00:00Z",
    ...cambios,
  };
}

function detalle(inscrito: boolean, estadoTorneo: string): DetalleEvento {
  return {
    evento: { evento_id: EVENTO, estado: "en_curso", estado_torneo: estadoTorneo },
    mi_inscripcion: { inscrito, devuelta: false },
  };
}

// ---------------------------------------------------------------------------
// Logica
// ---------------------------------------------------------------------------

describe("RF-18 · logica del ranking", () => {
  it("esRankingTorneo acepta el payload del motor y rechaza lo demas", () => {
    expect(esRankingTorneo(ranking())).toBe(true);
    expect(esRankingTorneo({ ...ranking(), emitido_en: "2026-11-03T20:00:01Z" })).toBe(true);
    expect(esRankingTorneo(null)).toBe(false);
    expect(esRankingTorneo({ ...ranking(), filas: [{ ...fila(YO), elegible: "no" }] })).toBe(false);
    expect(esRankingTorneo({ ...ranking(), botin: undefined })).toBe(false);
  });

  it("siguienteRanking descarta uno viejo del mismo evento; otro evento lo reemplaza", () => {
    const actual = ranking();
    const viejo = ranking({ actualizado_en: "2026-11-03T19:59:00Z", botin: 1 });
    expect(siguienteRanking(actual, viejo)).toBe(actual);
    const nuevo = ranking({ actualizado_en: "2026-11-03T20:00:05Z" });
    expect(siguienteRanking(actual, nuevo)).toBe(nuevo);
    const otro = ranking({ evento_id: "otro", actualizado_en: "2026-01-01T00:00:00Z" });
    expect(siguienteRanking(actual, otro)).toBe(otro);
    expect(siguienteRanking(null, viejo)).toBe(viejo);
  });

  it("etiquetas: Tú y Jugador N por usuario_id (no cambian al moverse la tabla)", () => {
    const e = etiquetas(ranking().filas, YO);
    expect(e.get(YO)).toBe("Tú");
    expect(e.get(OTRO_A)).toBe("Jugador 1");
    expect(e.get(OTRO_B)).toBe("Jugador 2");
    const reordenado = etiquetas([...ranking().filas].reverse(), YO);
    expect(reordenado.get(OTRO_A)).toBe("Jugador 1");
  });

  it("mostrarRanking: torneo = si; individual = no; sin saber, si esta en el ranking", () => {
    expect(mostrarRanking(ranking(), "torneo", YO)).toBe(true);
    expect(mostrarRanking(ranking(), "individual", YO)).toBe(false);
    expect(mostrarRanking(ranking(), null, YO)).toBe(true);
    expect(mostrarRanking(ranking({ filas: [fila(OTRO_A)] }), null, YO)).toBe(false);
    expect(mostrarRanking(null, "torneo", YO)).toBe(false);
    expect(mostrarRanking(ranking(), "torneo", null)).toBe(false);
  });

  it("anchoBarra se queda en 0..100", () => {
    expect(anchoBarra(50)).toBe(50);
    expect(anchoBarra(130)).toBe(100);
    expect(anchoBarra(-5)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// Panel en /carrera
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

let rutas: Record<string, { status: number; body?: unknown }>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  rutas = {};
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const r = rutas[`${init?.method ?? "GET"} ${url}`] ?? { status: 404, body: { error: "No existe", codigo: "no_existe" } };
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

describe("RF-18 · panel del ranking en /carrera", () => {
  it("sin ranking_torneo no se muestra", async () => {
    abrirCarrera();
    await screen.findByText(/aquí aparecerá una apuesta en vivo/);
    expect(screen.queryByTestId("ranking-torneo")).not.toBeInTheDocument();
  });

  it("inscrito con torneo activo: tabla con Tú resaltado, botín y barra de avance", async () => {
    rutas[`GET /api/eventos/${EVENTO}`] = { status: 200, body: detalle(true, "activo") };
    abrirCarrera();
    enviarWs("ranking_torneo", ranking());

    const panel = await screen.findByTestId("ranking-torneo");
    expect(within(panel).getByTestId("botin")).toHaveTextContent("Botín: 1.200 PoleCoins");
    const filas = within(panel).getAllByRole("row").slice(1);
    expect(filas.map((f) => f.textContent)).toEqual(["1Jugador 21.350", "2Tú1.100", "3Jugador 1700"]);
    expect(filas[1]).toHaveAttribute("aria-current", "true");
    expect(filas[0]).not.toHaveAttribute("aria-current");

    const avance = within(panel).getByTestId("avance");
    expect(avance).toHaveTextContent("Apostado: 250 de 500 fichas");
    expect(avance).toHaveTextContent("50 %");
    expect(within(panel).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "50");
    expect(avance).not.toHaveTextContent("Elegible");
  });

  it("al llegar al minimo dice Elegible; un ranking viejo no lo revierte", async () => {
    rutas[`GET /api/eventos/${EVENTO}`] = { status: 200, body: detalle(true, "activo") };
    abrirCarrera();
    enviarWs("ranking_torneo", ranking());
    await screen.findByTestId("ranking-torneo");

    const ya = ranking({ actualizado_en: "2026-11-03T20:00:10Z" });
    ya.filas[1] = fila(YO, { puesto: 1, fichas: 1500, apostado: 520, avance_pct: 100, elegible: true });
    enviarWs("ranking_torneo", ya);
    expect(screen.getByTestId("avance")).toHaveTextContent("Elegible");
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");

    enviarWs("ranking_torneo", ranking({ actualizado_en: "2026-11-03T19:59:00Z" }));
    expect(screen.getByTestId("avance")).toHaveTextContent("Elegible");
  });

  it("si el Orquestador dice que juega en individual, no se muestra", async () => {
    rutas[`GET /api/eventos/${EVENTO}`] = { status: 200, body: detalle(false, "activo") };
    abrirCarrera();
    enviarWs("ranking_torneo", ranking({ filas: [fila(OTRO_A), fila(OTRO_B)] }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => url === `/api/eventos/${EVENTO}`)).toBe(true));
    expect(screen.queryByTestId("ranking-torneo")).not.toBeInTheDocument();
  });

  it("sin la ruta del evento: se muestra solo si el jugador esta en el ranking", async () => {
    abrirCarrera();
    enviarWs("ranking_torneo", ranking({ filas: [fila(OTRO_A), fila(OTRO_B)] }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => url === `/api/eventos/${EVENTO}`)).toBe(true));
    expect(screen.queryByTestId("ranking-torneo")).not.toBeInTheDocument();

    enviarWs("ranking_torneo", ranking({ actualizado_en: "2026-11-03T20:00:05Z" }));
    expect(await screen.findByTestId("ranking-torneo")).toBeInTheDocument();
  });

  it("el reenvio cada 5 s del mismo ranking no pide de nuevo el evento", async () => {
    rutas[`GET /api/eventos/${EVENTO}`] = { status: 200, body: detalle(true, "activo") };
    abrirCarrera();
    for (let i = 0; i < 3; i++) enviarWs("ranking_torneo", ranking({ actualizado_en: `2026-11-03T20:00:0${i * 5}Z` }));
    await screen.findByTestId("ranking-torneo");
    expect(fetchMock.mock.calls.filter(([url]) => url === `/api/eventos/${EVENTO}`)).toHaveLength(1);
  });
});
