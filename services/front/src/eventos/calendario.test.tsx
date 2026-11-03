// Pruebas de HU2-05 en el front: calendario de eventos y detalle con mercados
// estáticos, contra el Orquestador (/api) con fetch simulado.
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { AuthProvider, type Usuario } from "../auth/AuthContext";
import { USUARIOS_SIMULADOS } from "../auth/usuariosPrueba";

type Respuesta = { status: number; body?: unknown } | Error;
let rutas: Record<string, Respuesta | Respuesta[]>;

beforeEach(() => {
  rutas = {};
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const clave = `${init?.method ?? "GET"} ${url.replace(/^\/api/, "")}`;
    const definida = rutas[clave];
    const r = (Array.isArray(definida) ? definida.shift() : definida) ?? { status: 404, body: {} };
    if (r instanceof Error) throw r;
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body ?? null };
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

const socketMudo = (): SocketLike => ({ onopen: null, onmessage: null, onclose: null, onerror: null, close() {} });

function abrir(usuario: Usuario = USUARIOS_SIMULADOS.player) {
  render(
    <MemoryRouter initialEntries={["/eventos"]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <AuthProvider usuarioInicial={usuario}>
        <WsProvider cliente={new ClienteWs({ url: "ws://prueba/ws", crearSocket: socketMudo })}>
          <App />
        </WsProvider>
      </AuthProvider>
    </MemoryRouter>,
  );
}

const LARGADA = new Date(Date.now() + 3600_000).toISOString();

const EVENTO = {
  evento_id: "evt-1",
  carrera: "2024_monza",
  estado: "programado",
  estado_torneo: "abierto",
  largada: LARGADA,
  inscritos: 3,
  botin: 300,
  costo_inscripcion: 100,
  fichas_iniciales: 1000,
};

describe("Calendario (HU2-05)", () => {
  it("lista los eventos con inscritos y botín", async () => {
    rutas["GET /eventos"] = { status: 200, body: { eventos: [EVENTO] } };
    abrir();

    expect(await screen.findByText("2024_monza")).toBeInTheDocument();
    expect(screen.getByText(/3 inscritos/)).toBeInTheDocument();
    expect(screen.getByText(/Bot[ií]n 300 PoleCoins/)).toBeInTheDocument();
  });

  it("al abrir un evento muestra sus mercados estáticos con multiplicadores", async () => {
    rutas["GET /eventos"] = { status: 200, body: { eventos: [EVENTO] } };
    rutas["GET /eventos/evt-1"] = {
      status: 200,
      body: {
        evento: EVENTO,
        mercados_estaticos: [
          {
            mercado_id: "m1",
            plantilla: "ganador",
            pregunta: "¿Quién gana la carrera?",
            estado: "abierto",
            cierra_en: LARGADA,
            opciones: [
              { opcion: "VER", multiplicador: 3 },
              { opcion: "NOR", multiplicador: 3 },
            ],
          },
        ],
        mi_inscripcion: { inscrito: false, devuelta: false },
      },
    };
    abrir();

    fireEvent.click(await screen.findByText("2024_monza"));

    expect(await screen.findByText("¿Quién gana la carrera?")).toBeInTheDocument();
    expect(screen.getByText("VER")).toBeInTheDocument();
    expect(screen.getAllByText("×3,0").length).toBeGreaterThanOrEqual(1);
  });

  it("muestra un aviso si el backend falla", async () => {
    rutas["GET /eventos"] = { status: 503, body: { error: "El servicio no está disponible, intenta de nuevo", codigo: "servicio_no_disponible" } };
    abrir();

    expect(await screen.findByText(/no está disponible/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /reintentar/i })).toBeInTheDocument();
  });
});
