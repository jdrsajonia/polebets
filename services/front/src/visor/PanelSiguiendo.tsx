// Tarjeta "Siguiendo" del mockup del visor (HU2-15): el piloto resaltado en el
// visor, con su puesto, sacado de la misma foto de posiciones de la tabla.

import type { Piloto } from "../api/tipos";

interface Props {
  piloto: Piloto | null;
}

export default function PanelSiguiendo({ piloto }: Props) {
  return (
    <section className="tarjeta" aria-labelledby="titulo-siguiendo" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <h2 id="titulo-siguiendo" className="titulo-seccion" style={{ margin: 0, color: "var(--text-muted)", fontSize: 15 }}>
        Siguiendo
      </h2>
      {piloto ? (
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ display: "block", width: 6, height: 40, background: piloto.color_equipo ?? "var(--border-strong)" }} />
          <div style={{ display: "flex", flexDirection: "column" }}>
            <span className="titulo-pagina" style={{ fontSize: 28, lineHeight: "30px" }}>
              {piloto.nombre || piloto.codigo}
            </span>
            <span className="texto-secundario">
              P{piloto.posicion} · {piloto.codigo} #{piloto.numero}
              {piloto.equipo && ` · ${piloto.equipo}`}
            </span>
          </div>
        </div>
      ) : (
        <p className="texto-secundario" style={{ margin: 0 }}>
          Elige un piloto en la tabla de posiciones para seguirlo en el visor.
        </p>
      )}
    </section>
  );
}
