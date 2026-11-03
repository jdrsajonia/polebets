// Apuesta dinamica en /carrera (HU2-08), en la columna central del mockup
// "Carrera en vivo": pregunta, botones Si/No con su multiplicador, monto con
// - y +, cuenta regresiva y lo que se recibe si se acierta, ANTES de
// confirmar (RF-09).
//
// La cuenta regresiva usa closes_at y es solo informativa: el motor cierra
// el mercado con el tiempo de carrera (RNF-19) y rechaza lo que llegue tarde.

import { useState } from "react";
import type { MensajeDinamico, ModoApuesta } from "../api/tipos";
import { formatoReloj, segundosHasta } from "../mercados/mercado";
import { useRelojServidor } from "../mercados/useMercado";
import { faseDinamica, pagoSiAcierta, TEXTO_FASE_DINAMICA, validarMonto, type FaseDinamica } from "./dinamico";
import { TEXTO_MODO, unidad } from "./modo";
import estilos from "./PanelDinamico.module.css";
import SelectorMonto from "./SelectorMonto";
import { useMercadoDinamico } from "./useMercadoDinamico";
import { useModoEvento } from "./useModoEvento";

const MONTO_INICIAL = "10";

export default function PanelDinamico() {
  const mercado = useMercadoDinamico();
  const modo = useModoEvento(mercado?.event_id ?? null);

  return (
    <section className="tarjeta" aria-labelledby="titulo-dinamica">
      <h2 id="titulo-dinamica" className="titulo-seccion">
        Apuesta en vivo
      </h2>
      {mercado ? (
        <MercadoDinamico key={mercado.market_id} mercado={mercado} modo={modo} />
      ) : (
        <p className="texto-secundario" style={{ margin: 0 }}>
          Cuando la carrera dé una oportunidad, aquí aparecerá una apuesta en vivo.
        </p>
      )}
    </section>
  );
}

interface Props {
  mercado: MensajeDinamico;
  modo: ModoApuesta | null;
}

function textoCuenta(fase: FaseDinamica, segundos: number, ganadora: string | null): string {
  if (fase === "resuelto") return `Ganó «${ganadora}»`;
  if (fase === "cerrado") return "Esperando el resultado…";
  return segundos > 0 ? `Cierra en ${formatoReloj(segundos)}` : "Cerrando…";
}

// Un mercado (key = market_id: al llegar otro, el formulario arranca limpio).
function MercadoDinamico({ mercado, modo }: Props) {
  const ahora = useRelojServidor(0);
  const [opcion, setOpcion] = useState<string | null>(null);
  const [texto, setTexto] = useState(MONTO_INICIAL);

  const fase = faseDinamica(mercado);
  const segundos = segundosHasta(mercado.closes_at, ahora);
  const abierto = fase === "abierto" && segundos > 0;
  const monto = validarMonto(texto);
  const elegida = mercado.options.find((o) => o.opcion === opcion) ?? null;

  return (
    <div className={estilos.panel} data-testid="mercado-dinamico">
      <div className={estilos.cabecera}>
        <span className={`etiqueta ${fase === "abierto" ? "" : estilos.etiquetaApagada}`} aria-live="polite" data-testid="fase-dinamica">
          {TEXTO_FASE_DINAMICA[fase]}
        </span>
        <span className={estilos.cuenta} data-testid="cuenta-dinamica">
          {textoCuenta(fase, segundos, mercado.winning_option)}
        </span>
      </div>

      <p className={estilos.pregunta}>{mercado.question}</p>

      <div className={estilos.opciones} role="group" aria-label="Opciones">
        {mercado.options.map((o) => {
          const clases = [estilos.opcion];
          if (o.opcion === opcion) clases.push(estilos.opcionElegida);
          if (o.opcion === mercado.winning_option) clases.push(estilos.opcionGanadora);
          return (
            <button
              key={o.opcion}
              type="button"
              className={clases.join(" ")}
              aria-pressed={o.opcion === opcion}
              onClick={() => setOpcion(o.opcion)}
              disabled={!abierto}
            >
              <span className={estilos.opcionNombre}>{o.opcion}</span>
              <span className={estilos.multiplicador}>× {o.multiplicador}</span>
            </button>
          );
        })}
      </div>

      {abierto && (
        <>
          <SelectorMonto valor={texto} onCambio={setTexto} />
          {!monto.ok && (
            <p className={estilos.error} role="alert">
              {monto.error}
            </p>
          )}
          <p className="texto-secundario" style={{ margin: 0 }} data-testid="modo-dinamica">
            {TEXTO_MODO[modo ?? "desconocido"]}
          </p>
          <p className={estilos.resumen} data-testid="pago-dinamica">
            {elegida && monto.ok
              ? `Si aciertas «${elegida.opcion}» recibes ${monto.monto} × ${elegida.multiplicador} = ${pagoSiAcierta(monto.monto, elegida.multiplicador)} ${unidad(modo)}.`
              : "Elige una opción y el monto para ver cuánto recibes si aciertas."}
          </p>
        </>
      )}
    </div>
  );
}
