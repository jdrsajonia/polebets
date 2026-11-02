// Pruebas de HU-01: registro de jugador desde el front (con fetch simulado).
//
// Cada `it` corresponde a un criterio de aceptacion de la historia.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WsProvider } from "../api/WsContexto";
import { ClienteWs, type SocketLike } from "../api/ws";
import App from "../App";
import { validarRegistro } from "../paginas/Registro";
import { MENSAJE_DATOS_INVALIDOS } from "./api";
import { AuthProvider, type Usuario } from "./AuthContext";

const NUEVO: Usuario = { id: "22222222-2222-4222-8222-222222222222", nombre: "Luis Gómez", email: "luis@arqui.local", rol: "player" };

// Gateway simulado (Usuarios y Orquestador): responde segun "METODO /ruta"; lo demas, 404.
type Respuesta = { status: number; body?: unknown } | Error;
let rutas: Record<string, Respuesta>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  rutas = {};
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const ruta = url.replace(/^\/api/, "");
    const r = rutas[`${init?.method ?? "GET"} ${ruta}`] ?? { status: 404, body: {} };
    if (r instanceof Error) throw r;
    return { ok: r.status >= 200 && r.status < 300, status: r.status, json: async () => r.body ?? null };
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

const socketMudo = (): SocketLike => ({ onopen: null, onmessage: null, onclose: null, onerror: null, close() {} });

function abrir(ruta: string) {
  render(
    <MemoryRouter initialEntries={[ruta]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <AuthProvider>
        <WsProvider cliente={new ClienteWs({ url: "ws://prueba/ws", crearSocket: socketMudo })}>
          <App />
        </WsProvider>
      </AuthProvider>
    </MemoryRouter>,
  );
}

async function llenarYEnviar(datos: { nombre: string; email: string; password: string; confirmacion: string }) {
  fireEvent.change(await screen.findByLabelText("Nombre"), { target: { value: datos.nombre } });
  fireEvent.change(screen.getByLabelText("Correo"), { target: { value: datos.email } });
  fireEvent.change(screen.getByLabelText("Contraseña"), { target: { value: datos.password } });
  fireEvent.change(screen.getByLabelText("Repite la contraseña"), { target: { value: datos.confirmacion } });
  fireEvent.click(screen.getByRole("button", { name: "Crear cuenta" }));
}

function llamadas(metodo: string, ruta: string) {
  return fetchMock.mock.calls.filter(([url, init]) => url === `/api${ruta}` && (init?.method ?? "GET") === metodo);
}

const SIN_SESION = { status: 401, body: { detail: "No hay sesion" } };

describe("HU-01 · registrarse", () => {
  it("con datos validos crea la cuenta, inicia sesion y entra a la carrera", async () => {
    rutas = {
      "GET /auth/me": SIN_SESION,
      "POST /registro": { status: 201, body: { usuario: NUEVO } },
      "POST /auth/login": { status: 200, body: NUEVO },
    };
    abrir("/registro");

    await llenarYEnviar({ nombre: "  Luis Gómez ", email: " luis@arqui.local ", password: "clave1234", confirmacion: "clave1234" });

    expect(await screen.findByRole("heading", { name: "Carrera en vivo" })).toBeInTheDocument();
    const [[, registro]] = llamadas("POST", "/registro");
    // Contrato del Orquestador (§3.1): nombre, email y contrasena, en español.
    expect(JSON.parse(registro.body)).toEqual({ nombre: "Luis Gómez", email: "luis@arqui.local", contrasena: "clave1234" });
    const [[, login]] = llamadas("POST", "/auth/login");
    expect(JSON.parse(login.body)).toEqual({ email: "luis@arqui.local", password: "clave1234" });
  });

  it("si el correo ya existe muestra el mensaje del Orquestador y no inicia sesion", async () => {
    rutas = {
      "GET /auth/me": SIN_SESION,
      "POST /registro": { status: 409, body: { error: "Este correo ya está registrado", codigo: "correo_registrado" } },
    };
    abrir("/registro");

    await llenarYEnviar({ nombre: "Luis", email: "luis@arqui.local", password: "clave1234", confirmacion: "clave1234" });

    expect(await screen.findByRole("alert")).toHaveTextContent("Este correo ya está registrado");
    expect(llamadas("POST", "/auth/login")).toHaveLength(0);
  });

  it("si el registro rechaza los datos (400 cuerpo_invalido) muestra un mensaje legible", async () => {
    rutas = {
      "GET /auth/me": SIN_SESION,
      "POST /registro": { status: 400, body: { error: "Revisa los datos enviados", codigo: "cuerpo_invalido" } },
    };
    abrir("/registro");

    await llenarYEnviar({ nombre: "Luis", email: "luis@arqui.local", password: "clave1234", confirmacion: "clave1234" });

    expect(await screen.findByRole("alert")).toHaveTextContent(MENSAJE_DATOS_INVALIDOS);
  });

  it("valida los campos antes de llamar al registro", async () => {
    rutas = { "GET /auth/me": SIN_SESION };
    abrir("/registro");

    await llenarYEnviar({ nombre: "", email: "no-es-correo", password: "corta", confirmacion: "otra" });

    expect(await screen.findByText("Escribe tu nombre.")).toBeInTheDocument();
    expect(screen.getByText("Escribe un correo válido.")).toBeInTheDocument();
    expect(screen.getByText("La contraseña debe tener al menos 8 caracteres.")).toBeInTheDocument();
    expect(screen.getByText("Las contraseñas no coinciden.")).toBeInTheDocument();
    expect(llamadas("POST", "/registro")).toHaveLength(0);
  });

  it("si la cuenta se crea pero falla el login automatico, lleva a /login", async () => {
    rutas = {
      "GET /auth/me": SIN_SESION,
      "POST /registro": { status: 201, body: { usuario: NUEVO } },
      "POST /auth/login": new TypeError("Failed to fetch"),
    };
    abrir("/registro");

    await llenarYEnviar({ nombre: "Luis", email: "luis@arqui.local", password: "clave1234", confirmacion: "clave1234" });

    expect(await screen.findByRole("heading", { name: "Iniciar sesión" })).toBeInTheDocument();
  });

  it("con sesion abierta, /registro lleva al inicio", async () => {
    rutas = { "GET /auth/me": { status: 200, body: NUEVO } };
    abrir("/registro");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Carrera en vivo" })).toBeInTheDocument());
  });
});

describe("HU-01 · reglas de validacion (iguales a Pagos)", () => {
  it("acepta datos validos", () => {
    expect(validarRegistro("Ana", "ana@arqui.local", "12345678", "12345678")).toEqual({});
  });

  it("limita el nombre a 100 caracteres y la contraseña a 128", () => {
    const e = validarRegistro("a".repeat(101), "ana@arqui.local", "x".repeat(129), "x".repeat(129));
    expect(e.nombre).toBeDefined();
    expect(e.password).toBeDefined();
  });
});
