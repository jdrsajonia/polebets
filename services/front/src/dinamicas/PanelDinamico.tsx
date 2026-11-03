// Apuesta dinamica en /carrera (HU2-08), en la columna central del mockup
// "Carrera en vivo": pregunta, botones Si/No con su multiplicador, monto con
// - y +, cuenta regresiva y lo que se recibe si se acierta, ANTES de
// confirmar (RF-09). Confirmar llama a POST /api/apuestas con una
// Idempotency-Key por intento (apuesta.ts).
//
// La cuenta regresiva usa closes_at y es solo informativa: el motor cierra
// el mercado con el tiempo de carrera (RNF-19) y rechaza lo que llegue tarde.

import { useEffect, useState } from "react";
import {
  TIPOS_WS,
  type MensajeDinamico,
  type MiApuesta,
  type ModoApuesta,
  type RespuestaApuesta,
  type ResultadoDinamico,
} from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { useBilletera } from "../billetera/BilleteraContexto";
import { formatoReloj, segundosHasta } from "../mercados/mercado";
import { useRelojServidor } from "../mercados/useMercado";
import { apostar, obtenerMiApuesta } from "./api";
import { bloqueaElMercado, claveParaIntento, mensajeErrorApuesta, textoApuesta, textoMiApuesta, type ClaveIntento } from "./apuesta";
import { faseDinamica, pagoSiAcierta, TEXTO_FASE_DINAMICA, validarMonto, type FaseDinamica } from "./dinamico";
import { TEXTO_MODO, unidad } from "./modo";
import estilos from "./PanelDinamico.module.css";
import { esResultadoDinamico, estaLiquidada, textoLiquidada, textoResultado } from "./resultado";
import SelectorMonto from "./SelectorMonto";
import { useMercadoDinamico } from "./useMercadoDinamico";
import { useModoEvento } from "./useModoEvento";

const MONTO_INICIAL = "10";

// Resuelto sin resultado_apuesta: cada cuanto y cuantas veces se vuelve a
// pedir mi-apuesta. Exportados para las pruebas.
export const ESPERA_RESULTADO_MS = 5000;
export const REINTENTOS_RESULTADO = 3;

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
  const { recargar: recargarSaldo } = useBilletera();
  const { suscribir } = useWs();
  const [opcion, setOpcion] = useState<string | null>(null);
  const [texto, setTexto] = useState(MONTO_INICIAL);
  const [clave, setClave] = useState<ClaveIntento | null>(null);
  const [enviando, setEnviando] = useState(false);
  const [apuesta, setApuesta] = useState<RespuestaApuesta | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [bloqueado, setBloqueado] = useState(false);
  const [resultado, setResultado] = useState<ResultadoDinamico | null>(null);
  // Lo que devolvio mi-apuesta (al montar o al reintentar tras resolverse).
  const [previa, setPrevia] = useState<MiApuesta | null>(null);

  useEffect(() => {
    const control = new AbortController();
    obtenerMiApuesta(mercado.market_id, control.signal)
      .then((a) => a && setPrevia(a))
      .catch(() => {}); // sin dato: se puede apostar; el motor rechaza si ya aposto
    return () => control.abort();
  }, [mercado.market_id]);

  // El de otro mercado (o el del P1, sin modo) se ignora. El saldo del header
  // lo recarga RecargaPorResultado.
  useEffect(
    () =>
      suscribir<unknown>(TIPOS_WS.resultadoApuesta, (payload) => {
        if (esResultadoDinamico(payload) && payload.mercado_id === mercado.market_id) setResultado(payload);
      }),
    [suscribir, mercado.market_id],
  );

  const fase = faseDinamica(mercado);
  const liquidada = previa !== null && estaLiquidada(previa);
  const tieneApuesta = apuesta !== null || previa !== null;
  const esperandoLiquidacion = fase === "resuelto" && tieneApuesta && !resultado && !liquidada;

  useEffect(() => {
    if (!esperandoLiquidacion) return;
    const control = new AbortController();
    let intentos = 0;
    let temporizador: ReturnType<typeof setTimeout>;
    const programar = () => {
      temporizador = setTimeout(() => void intentar(), ESPERA_RESULTADO_MS);
    };
    async function intentar() {
      intentos++;
      try {
        const a = await obtenerMiApuesta(mercado.market_id, control.signal);
        if (a && estaLiquidada(a)) {
          setPrevia(a);
          if (a.modo === "individual") recargarSaldo(); // tampoco llego al header
          return;
        }
      } catch {
        if (control.signal.aborted) return;
      }
      if (intentos < REINTENTOS_RESULTADO) programar();
    }
    programar();
    return () => {
      clearTimeout(temporizador);
      control.abort();
    };
  }, [esperandoLiquidacion, mercado.market_id, recargarSaldo]);

  const segundos = segundosHasta(mercado.closes_at, ahora);
  const abierto = fase === "abierto" && segundos > 0;
  const editable = abierto && !tieneApuesta && !resultado && !bloqueado && !enviando;
  const monto = validarMonto(texto);
  const elegida = mercado.options.find((o) => o.opcion === opcion) ?? null;
  const puedeConfirmar = editable && elegida !== null && monto.ok;

  async function confirmar() {
    if (!puedeConfirmar || !elegida || !monto.ok) return;
    // Misma clave mientras no cambien mercado, opcion ni monto (reintentos).
    const actual = claveParaIntento(clave, { mercado_id: mercado.market_id, opcion: elegida.opcion, monto: monto.monto });
    setClave(actual);
    setEnviando(true);
    setError(null);
    try {
      const r = await apostar(actual.intento, actual.clave);
      setApuesta(r);
      setClave(null); // exito: la proxima apuesta lleva otra clave
      if (r.modo === "individual") recargarSaldo();
    } catch (causa) {
      setError(mensajeErrorApuesta(causa));
      if (bloqueaElMercado(causa)) setBloqueado(true);
    } finally {
      setEnviando(false);
    }
  }

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
          const mia = resultado?.opcion ?? apuesta?.opcion ?? previa?.opcion ?? opcion;
          if (o.opcion === mia) clases.push(estilos.opcionElegida);
          if (o.opcion === mercado.winning_option) clases.push(estilos.opcionGanadora);
          return (
            <button
              key={o.opcion}
              type="button"
              className={clases.join(" ")}
              aria-pressed={o.opcion === mia}
              onClick={() => setOpcion(o.opcion)}
              disabled={!editable}
            >
              <span className={estilos.opcionNombre}>{o.opcion}</span>
              <span className={estilos.multiplicador}>× {o.multiplicador}</span>
            </button>
          );
        })}
      </div>

      {abierto && !tieneApuesta && !resultado && (
        <>
          <SelectorMonto valor={texto} onCambio={setTexto} deshabilitado={!editable} />
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
          <button type="button" className="boton" onClick={() => void confirmar()} disabled={!puedeConfirmar}>
            {enviando ? "Enviando…" : "Confirmar apuesta"}
          </button>
        </>
      )}

      {error && (
        <p className={estilos.error} role="alert" data-testid="error-dinamica">
          {error}
        </p>
      )}
      {resultado ? (
        <p className={resultado.gano ? estilos.ok : estilos.error} role="status" data-testid="resultado-dinamica">
          {textoResultado(resultado)}
        </p>
      ) : previa && liquidada ? (
        <p className={previa.status === "won" ? estilos.ok : estilos.error} role="status" data-testid="resultado-dinamica">
          {textoLiquidada(previa)}
        </p>
      ) : apuesta ? (
        <p className={estilos.ok} role="status" data-testid="apuesta-dinamica">
          {textoApuesta(apuesta, fase !== "abierto")}
        </p>
      ) : (
        previa && (
          <p className={estilos.ok} role="status" data-testid="apuesta-dinamica">
            {textoMiApuesta(previa, fase !== "abierto")}
          </p>
        )
      )}
    </div>
  );
}
