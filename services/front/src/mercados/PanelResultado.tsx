// Tarjeta "Resultado de tu apuesta" de /carrera (HU-08).
//
//   - resultado_apuesta: ganaste/perdiste, tu opcion vs la ganadora, puntos
//     obtenidos y puntaje total. Ademas recarga el saldo del header.
//   - mercado_liquidado sin resultado antes: "No apostaste en este mercado".
//   - mercado_abierto: limpia el panel para el mercado nuevo.

import { useEffect, useState } from "react";
import { TIPOS_WS } from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { useBilletera } from "../billetera/BilleteraContexto";
import estilos from "./PanelResultado.module.css";
import { alLiquidarse, esResultadoApuesta, ESPERANDO, formatoPuntos, type VistaResultado } from "./resultado";

export default function PanelResultado() {
  const { suscribir } = useWs();
  const { recargar } = useBilletera();
  const [vista, setVista] = useState<VistaResultado>(ESPERANDO);

  useEffect(() => {
    const quitar = [
      suscribir<unknown>(TIPOS_WS.resultadoApuesta, (payload) => {
        if (!esResultadoApuesta(payload)) return;
        setVista({ tipo: "resultado", resultado: payload });
        recargar();
      }),
      suscribir<unknown>(TIPOS_WS.mercadoLiquidado, (payload) => {
        const p = (payload ?? {}) as { mercado_id?: unknown; id?: unknown };
        setVista((v) => alLiquidarse(v, p.mercado_id ?? p.id));
      }),
      suscribir<unknown>(TIPOS_WS.mercadoAbierto, () => setVista(ESPERANDO)),
    ];
    return () => quitar.forEach((f) => f());
  }, [suscribir, recargar]);

  let contenido;
  if (vista.tipo === "resultado") {
    const r = vista.resultado;
    contenido = (
      <div className={estilos.resultado}>
        <p className={`${estilos.veredicto} ${r.gano ? estilos.gano : estilos.perdio}`}>
          {r.gano ? "¡Ganaste!" : "Perdiste"}
        </p>
        <dl className={estilos.datos}>
          <div>
            <dt>Tu opción</dt>
            <dd>{r.opcion}</dd>
          </div>
          <div>
            <dt>Opción ganadora</dt>
            <dd>{r.opcion_ganadora}</dd>
          </div>
          <div>
            <dt>Puntos obtenidos</dt>
            <dd className={r.gano ? estilos.puntosGanados : undefined}>
              {r.gano ? "+" : ""}
              {formatoPuntos(r.puntos_obtenidos)}
            </dd>
          </div>
          <div>
            <dt>Puntaje total</dt>
            <dd>{formatoPuntos(r.puntaje_total)}</dd>
          </div>
        </dl>
      </div>
    );
  } else if (vista.tipo === "sin_apuesta") {
    contenido = <p className="texto-secundario">No apostaste en este mercado.</p>;
  } else {
    contenido = (
      <p className="texto-secundario">Cuando se liquide el mercado verás aquí si acertaste y los puntos obtenidos.</p>
    );
  }

  return (
    <section className="tarjeta" aria-labelledby="titulo-resultado">
      <h2 id="titulo-resultado" className="titulo-seccion">
        Resultado de tu apuesta
      </h2>
      {/* aria-live: el lector de pantalla anuncia el resultado al llegar. */}
      <div aria-live="polite" className={estilos.contenido}>
        {contenido}
      </div>
    </section>
  );
}
