// Tabla de posiciones de HU-05, con el diseño del mockup "Carrera en vivo":
// posicion, color del equipo, numero, codigo, nombre y diferencia con el lider.
// La fila de un piloto que acaba de cambiar de puesto se resalta un momento.
// Si la pagina pasa alSeguir (HU2-15), el piloto es un boton que lo resalta en
// el visor de pista, y su fila queda marcada.

import type { Piloto } from "../api/tipos";
import type { Cambios } from "./foto";
import estilos from "./TablaPosiciones.module.css";

interface Props {
  pilotos: Piloto[]; // ya ordenados por posicion
  cambios?: Cambios; // pilotos a resaltar, por numero de carro
  seguido?: number | null; // piloto que sigue el visor (HU2-15)
  alSeguir?: (numero: number) => void; // sin ella la tabla no es interactiva
}

const SIN_CAMBIOS: Cambios = new Map();

export default function TablaPosiciones({ pilotos, cambios = SIN_CAMBIOS, seguido = null, alSeguir }: Props) {
  return (
    <table className={estilos.tabla}>
      {/* Encabezados solo para lectores de pantalla: el mockup no los muestra. */}
      <thead className="solo-lectores">
        <tr>
          <th scope="col">Posición</th>
          <th scope="col">Equipo</th>
          <th scope="col">Número</th>
          <th scope="col">Piloto</th>
          <th scope="col">Diferencia con el líder</th>
        </tr>
      </thead>
      <tbody>
        {pilotos.map((p) => {
          const movimiento = cambios.get(p.numero);
          const clases = [
            estilos.fila,
            movimiento === "sube" && estilos.sube,
            movimiento === "baja" && estilos.baja,
            seguido === p.numero && estilos.seguido,
          ]
            .filter(Boolean)
            .join(" ");
          return (
            // La key es el numero del carro: asi React mueve la fila en vez de
            // reescribirla cuando el piloto cambia de puesto.
            <tr key={p.numero} className={clases} data-movimiento={movimiento}>
              <td className={estilos.posicion}>{p.posicion}</td>
              <td className={estilos.celdaEquipo}>
                <span className={estilos.barraEquipo} style={{ background: p.color_equipo ?? "var(--border-strong)" }} title={p.equipo} />
                <span className="solo-lectores">{p.equipo}</span>
              </td>
              <td className={estilos.numero}>{p.numero}</td>
              <td className={estilos.piloto}>
                {alSeguir ? (
                  <button
                    type="button"
                    className={estilos.botonSeguir}
                    aria-pressed={seguido === p.numero}
                    title={`Seguir a ${p.codigo} en el visor`}
                    onClick={() => alSeguir(p.numero)}
                  >
                    <b>{p.codigo}</b> <span className={estilos.nombre}>{p.nombre}</span>
                  </button>
                ) : (
                  <>
                    <b>{p.codigo}</b> <span className={estilos.nombre}>{p.nombre}</span>
                  </>
                )}
                {movimiento && (
                  <span className={estilos.flecha} aria-label={movimiento === "sube" ? "subió" : "bajó"}>
                    {movimiento === "sube" ? "▲" : "▼"}
                  </span>
                )}
              </td>
              <td className={estilos.diferencia}>{p.diferencia}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
