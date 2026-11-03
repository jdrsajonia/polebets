// HU2-15: tarjeta del visor de pista y tabla de posiciones para elegir a quien seguir.

import { act, fireEvent, render, screen } from "@testing-library/react";
import type { Piloto } from "../api/tipos";
import TablaPosiciones from "../posiciones/TablaPosiciones";
import PanelSiguiendo from "./PanelSiguiendo";
import VisorPista from "./VisorPista";

const PILOTOS: Piloto[] = [
  { posicion: 1, numero: 55, codigo: "SAI", nombre: "Carlos SAINZ", equipo: "Ferrari", color_equipo: "#E80020", diferencia: "Líder" },
  { posicion: 2, numero: 4, codigo: "NOR", nombre: "Lando NORRIS", equipo: "McLaren", diferencia: "—" },
];

// El elemento <visor-pista> que quedo en el documento.
function elementoVisor(): HTMLElement {
  const el = document.querySelector("visor-pista");
  if (!el) throw new Error("no se dibujo <visor-pista>");
  return el as HTMLElement;
}

describe("VisorPista", () => {
  it("mientras carga Blazor avisa y luego incrusta el elemento con el piloto a seguir", async () => {
    let terminar = () => {};
    const cargar = vi.fn(() => new Promise<void>((resolve) => (terminar = resolve)));
    const { rerender } = render(<VisorPista seguido={55} cargar={cargar} />);
    expect(screen.getByRole("status")).toHaveTextContent("Cargando el visor");

    await act(async () => terminar());
    expect(elementoVisor()).toHaveAttribute("seguir", "55");
    expect(elementoVisor()).toHaveAttribute("mostrar-codigos", "true");

    // Elegir otro piloto solo cambia el atributo: Blazor lo observa.
    rerender(<VisorPista seguido={4} cargar={cargar} />);
    expect(elementoVisor()).toHaveAttribute("seguir", "4");
    expect(cargar).toHaveBeenCalledTimes(1);
  });

  it("sin piloto seguido manda 0", async () => {
    await act(async () => {
      render(<VisorPista seguido={null} cargar={() => Promise.resolve()} />);
    });
    expect(elementoVisor()).toHaveAttribute("seguir", "0");
  });

  it("el boton oculta los codigos", async () => {
    await act(async () => {
      render(<VisorPista seguido={55} cargar={() => Promise.resolve()} />);
    });
    const boton = screen.getByRole("button", { name: "Mostrar códigos" });
    expect(boton).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(boton);
    expect(boton).toHaveAttribute("aria-pressed", "false");
    expect(elementoVisor()).toHaveAttribute("mostrar-codigos", "false");
  });

  it("si el visor no carga lo dice y permite reintentar", async () => {
    const cargar = vi.fn().mockRejectedValueOnce(new Error("No se pudo descargar el visor")).mockResolvedValueOnce(undefined);
    await act(async () => {
      render(<VisorPista seguido={55} cargar={cargar} />);
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Visor no disponible: No se pudo descargar el visor");
    expect(document.querySelector("visor-pista")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    });
    expect(cargar).toHaveBeenCalledTimes(2);
    expect(elementoVisor()).toHaveAttribute("seguir", "55");
  });
});

describe("TablaPosiciones con seguimiento", () => {
  it("tocar un piloto lo elige y su fila queda marcada", () => {
    const alSeguir = vi.fn();
    render(<TablaPosiciones pilotos={PILOTOS} seguido={55} alSeguir={alSeguir} />);
    const sainz = screen.getByRole("button", { name: /SAI/ });
    expect(sainz).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: /NOR/ }));
    expect(alSeguir).toHaveBeenCalledWith(4);
  });

  it("sin alSeguir la tabla sigue sin botones (como en HU-05)", () => {
    render(<TablaPosiciones pilotos={PILOTOS} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("SAI")).toBeInTheDocument();
  });
});

describe("PanelSiguiendo", () => {
  it("muestra el piloto seguido con su puesto", () => {
    render(<PanelSiguiendo piloto={PILOTOS[1]} />);
    expect(screen.getByText("Lando NORRIS")).toBeInTheDocument();
    expect(screen.getByText(/P2 · NOR #4 · McLaren/)).toBeInTheDocument();
  });

  it("sin piloto invita a elegir uno", () => {
    render(<PanelSiguiendo piloto={null} />);
    expect(screen.getByText(/Elige un piloto/)).toBeInTheDocument();
  });
});
