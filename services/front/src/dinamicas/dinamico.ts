// Logica de HU2-08 en el front, sin React para poder probarla sola.
//
// El ciclo del mercado dinamico lo decide el motor con el tiempo de carrera
// (RNF-19). El front solo refleja los mensajes "mercado_dinamico":
//   abierto  -> se muestra y se puede apostar
//   cerrado  -> termino la ventana; se observa la carrera
//   resuelto -> llego winning_option: se muestra el resultado del mercado

import type { MensajeDinamico } from "../api/tipos";

export type FaseDinamica = "abierto" | "cerrado" | "resuelto";

export const TEXTO_FASE_DINAMICA: Record<FaseDinamica, string> = {
  abierto: "Abierto",
  cerrado: "Cerrado · en observación",
  resuelto: "Resuelto",
};

// Revisa que lo que llego por la red tenga la forma del mensaje.
export function esMensajeDinamico(x: unknown): x is MensajeDinamico {
  if (typeof x !== "object" || x === null) return false;
  const m = x as Record<string, unknown>;
  return (
    typeof m.market_id === "string" &&
    typeof m.event_id === "string" &&
    typeof m.question === "string" &&
    (m.status === "abierto" || m.status === "cerrado") &&
    typeof m.closes_at === "string" &&
    Array.isArray(m.options) &&
    m.options.every(
      (o) =>
        typeof o === "object" &&
        o !== null &&
        typeof (o as Record<string, unknown>).opcion === "string" &&
        typeof (o as Record<string, unknown>).multiplicador === "number",
    ) &&
    (m.winning_option === null || m.winning_option === undefined || typeof m.winning_option === "string")
  );
}

export function faseDinamica(m: MensajeDinamico): FaseDinamica {
  if (m.winning_option) return "resuelto";
  return m.status === "abierto" ? "abierto" : "cerrado";
}

const ORDEN: Record<FaseDinamica, number> = { abierto: 0, cerrado: 1, resuelto: 2 };

// Siguiente mercado en pantalla al llegar `msg`:
//   - abierto: es el mercado nuevo (el motor abre uno a la vez).
//   - cerrado o resuelto de OTRO mercado: se ignora (p. ej. el cierre de uno
//     que se abrio antes de entrar a la pagina, que nunca se vio abierto).
//   - del mismo mercado: avanza de fase; nunca retrocede (un mensaje viejo
//     que llega tarde no reabre un mercado cerrado).
export function siguienteMercado(actual: MensajeDinamico | null, msg: MensajeDinamico): MensajeDinamico | null {
  const fase = faseDinamica(msg);
  if (!actual || actual.market_id !== msg.market_id) {
    return fase === "abierto" ? msg : actual;
  }
  return ORDEN[fase] >= ORDEN[faseDinamica(actual)] ? msg : actual;
}
