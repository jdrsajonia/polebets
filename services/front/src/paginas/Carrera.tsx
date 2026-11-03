// Pantalla /carrera: la carrera en vivo. Sigue el mockup "Carrera en vivo":
// cabecera, posiciones a la izquierda (HU-05) y el mercado en el centro
// (HU-11 con el formulario de HU-06) con el resultado de la apuesta (HU-08).
// Arriba en el centro va el visor de pista (HU2-15): tocar un piloto en la
// tabla lo sigue en el visor.

import { useState } from "react";
import { MOSTRAR_LATENCIA } from "../api/config";
import { useWs } from "../api/WsContexto";
import PanelDinamico from "../dinamicas/PanelDinamico";
import MercadoJugador from "../mercados/MercadoJugador";
import PanelResultado from "../mercados/PanelResultado";
import AvisoConexion from "../posiciones/AvisoConexion";
import PanelPosiciones from "../posiciones/PanelPosiciones";
import { usePosiciones } from "../posiciones/usePosiciones";
import PanelRanking from "../torneo/PanelRanking";
import PanelSiguiendo from "../visor/PanelSiguiendo";
import VisorPista from "../visor/VisorPista";
import estilos from "./Carrera.module.css";

export default function Carrera() {
  const { estado } = useWs();
  const posiciones = usePosiciones();
  const { foto, fase, latenciaMs } = posiciones;
  const enVivo = fase === "lista" && estado === "abierta";
  // HU2-15: piloto que sigue el visor. Mientras el jugador no elija, el lider.
  const [elegido, setElegido] = useState<number | null>(null);
  const pilotos = foto?.pilotos ?? [];
  const seguido = pilotos.find((p) => p.numero === elegido) ?? pilotos[0] ?? null;

  return (
    <>
      {/* Cabecera: estado de la carrera y, si se pide, la latencia (RNF-04) */}
      <section className={`tarjeta ${estilos.cabecera}`}>
        <div className={estilos.cabeceraTitulo}>
          <span className={enVivo ? "etiqueta" : `etiqueta ${estilos.etiquetaApagada}`}>
            <span className={estilos.puntoVivo} aria-hidden="true" />
            {enVivo ? "En vivo" : "Sin señal"}
          </span>
          <div>
            <h1 className="titulo-pagina" style={{ fontSize: 30 }}>
              Carrera en vivo
            </h1>
            <span className="texto-secundario">
              {foto ? [foto.vuelta !== undefined && (foto.total_vueltas ? `Vuelta ${foto.vuelta}/${foto.total_vueltas}` : `Vuelta ${foto.vuelta}`), foto.carrera_id].filter(Boolean).join(" · ") : "Esperando datos de la carrera"}
            </span>
          </div>
        </div>
        {MOSTRAR_LATENCIA && (
          <span className={estilos.latencia} title="Tiempo entre que el motor emitió el último mensaje y llegó a esta pantalla">
            Latencia: <b>{latenciaMs === null ? "—" : `${latenciaMs} ms`}</b>
          </span>
        )}
      </section>

      <AvisoConexion estado={estado} />

      <div className={estilos.columnas}>
        {/* Columna izquierda: HU-05 y, si esta inscrito, el ranking del torneo (RF-18) */}
        <aside className={estilos.lateral}>
          <PanelPosiciones posiciones={posiciones} seguido={seguido?.numero ?? null} alSeguir={setElegido} />
          <PanelSiguiendo piloto={seguido} />
          <PanelRanking />
        </aside>

        {/* Columna central: visor de pista (HU2-15), apuesta dinamica (HU2-08),
            mercado del P1 (HU-11, con el formulario de HU-06) y el resultado
            de la apuesta (HU-08) */}
        <div className={estilos.centro}>
          <VisorPista seguido={seguido?.numero ?? null} />
          <PanelDinamico />
          <MercadoJugador />
          <PanelResultado />
        </div>
      </div>
    </>
  );
}
