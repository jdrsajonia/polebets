// Apuesta estatica en el detalle del evento (front de HU2-09, RF-09/RF-12):
// las opciones del mercado con su multiplicador, monto (SelectorMonto, maximo
// 500) y, ANTES de confirmar, el modo, el multiplicador y cuanto recibe si
// acierta. Confirmar llama a POST /api/apuestas con una Idempotency-Key por
// intento, como el panel dinamico. Las estaticas siempre son individuales.
//
// Al montarse pide mi-apuesta (GET /api/mercados/{id}/mi-apuesta): si el
// jugador ya aposto, la muestra (o el resultado, si ya se liquido) y no deja
// apostar otra vez. Con el mercado cerrado no hay formulario: "Cerrado en la
// largada".

import { useEffect, useState } from "react";
import type { MiApuesta, RespuestaApuesta } from "../api/tipos";
import { useBilletera } from "../billetera/BilleteraContexto";
import { apostar, obtenerMiApuesta } from "../dinamicas/api";
import { bloqueaElMercado, claveParaIntento, mensajeErrorApuesta, textoApuesta, textoMiApuesta, type ClaveIntento } from "../dinamicas/apuesta";
import { estaLiquidada, textoLiquidada } from "../dinamicas/resultado";
import SelectorMonto from "../dinamicas/SelectorMonto";
import type { MercadoEstatico } from "./api";
import estilos from "./ApuestaEstatica.module.css";
import {
  estaAbierto,
  formatoMult,
  TEXTO_AUN_NO_ABRE,
  TEXTO_CERRADO,
  textoAntesDeConfirmar,
  validarMontoEstatico,
} from "./apuestaEstatica";

const MONTO_INICIAL = "10";

export default function ApuestaEstatica({ mercado }: { mercado: MercadoEstatico }) {
  const { recargar: recargarSaldo } = useBilletera();
  const [opcion, setOpcion] = useState<string | null>(null);
  const [texto, setTexto] = useState(MONTO_INICIAL);
  const [clave, setClave] = useState<ClaveIntento | null>(null);
  const [enviando, setEnviando] = useState(false);
  const [apuesta, setApuesta] = useState<RespuestaApuesta | null>(null);
  const [previa, setPrevia] = useState<MiApuesta | null>(null);
  const [cargandoPrevia, setCargandoPrevia] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [bloqueado, setBloqueado] = useState(false);

  useEffect(() => {
    const control = new AbortController();
    obtenerMiApuesta(mercado.mercado_id, control.signal)
      .then((a) => a && setPrevia(a))
      .catch(() => {}) // sin dato: se puede apostar; el Motor rechaza si ya aposto
      .finally(() => {
        if (!control.signal.aborted) setCargandoPrevia(false);
      });
    return () => control.abort();
  }, [mercado.mercado_id]);

  const abierto = estaAbierto(mercado);
  const yaAposto = apuesta !== null || previa !== null;
  const editable = abierto && !yaAposto && !cargandoPrevia && !bloqueado && !enviando;
  const monto = validarMontoEstatico(texto);
  const elegida = mercado.opciones.find((o) => o.opcion === opcion) ?? null;
  const puedeConfirmar = editable && elegida !== null && monto.ok;
  const mia = apuesta?.opcion ?? previa?.opcion ?? opcion;

  async function confirmar() {
    if (!puedeConfirmar || !elegida || !monto.ok) return;
    // Misma clave mientras no cambien mercado, opcion ni monto (reintentos).
    const actual = claveParaIntento(clave, { mercado_id: mercado.mercado_id, opcion: elegida.opcion, monto: monto.monto });
    setClave(actual);
    setEnviando(true);
    setError(null);
    try {
      const r = await apostar(actual.intento, actual.clave);
      setApuesta(r);
      setClave(null); // exito: la proxima apuesta lleva otra clave
      recargarSaldo();
    } catch (causa) {
      setError(mensajeErrorApuesta(causa));
      if (bloqueaElMercado(causa)) setBloqueado(true);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className={estilos.apuesta} data-testid={`apuesta-estatica-${mercado.mercado_id}`}>
      <div className={estilos.opciones} role="group" aria-label={`Opciones de ${mercado.pregunta}`}>
        {mercado.opciones.map((o) => (
          <button
            key={o.opcion}
            type="button"
            className={o.opcion === mia ? `${estilos.opcion} ${estilos.opcionElegida}` : estilos.opcion}
            aria-pressed={o.opcion === mia}
            onClick={() => setOpcion(o.opcion)}
            disabled={!editable}
          >
            <span>{o.opcion}</span>
            <b>×{formatoMult.format(o.multiplicador)}</b>
          </button>
        ))}
      </div>

      {!abierto && (
        <p className="texto-secundario" data-testid="estado-estatica">
          {mercado.estado === "programado" ? TEXTO_AUN_NO_ABRE : TEXTO_CERRADO}
        </p>
      )}

      {abierto && !yaAposto && !cargandoPrevia && (
        <>
          <SelectorMonto id={`monto-${mercado.mercado_id}`} valor={texto} onCambio={setTexto} deshabilitado={!editable} />
          {!monto.ok && (
            <p className={estilos.error} role="alert">
              {monto.error}
            </p>
          )}
          <p className={estilos.resumen} data-testid="resumen-estatica">
            {elegida && monto.ok
              ? textoAntesDeConfirmar(elegida.opcion, elegida.multiplicador, monto.monto)
              : "Modo individual: elige una opción y el monto para ver cuánto recibes si aciertas."}
          </p>
          <button type="button" className="boton" onClick={() => void confirmar()} disabled={!puedeConfirmar}>
            {enviando ? "Enviando…" : "Confirmar apuesta"}
          </button>
        </>
      )}

      {error && (
        <p className={estilos.error} role="alert" data-testid="error-estatica">
          {error}
        </p>
      )}
      {apuesta ? (
        <p className={estilos.ok} role="status" data-testid="mi-apuesta-estatica">
          {textoApuesta(apuesta, false)}
        </p>
      ) : (
        previa && (
          <p className={estaLiquidada(previa) && previa.status === "lost" ? estilos.error : estilos.ok} role="status" data-testid="mi-apuesta-estatica">
            {estaLiquidada(previa) ? textoLiquidada(previa) : textoMiApuesta(previa, !abierto)}
          </p>
        )
      )}
    </div>
  );
}
