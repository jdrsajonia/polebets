// Logica de HU-08 (resultado de la apuesta), sin React, para probarla sola.

import type { ResultadoApuesta } from "../api/tipos";

// Lo que muestra el panel "Resultado de tu apuesta".
export type VistaResultado =
  | { tipo: "esperando" } // aun no se liquida el mercado
  | { tipo: "resultado"; resultado: ResultadoApuesta }
  | { tipo: "sin_apuesta" }; // se liquido y el jugador no aposto

export const ESPERANDO: VistaResultado = { tipo: "esperando" };

// Valida el payload de resultado_apuesta antes de pintarlo.
export function esResultadoApuesta(x: unknown): x is ResultadoApuesta {
  if (typeof x !== "object" || x === null) return false;
  const r = x as Record<string, unknown>;
  return (
    typeof r.mercado_id === "string" &&
    typeof r.apuesta_id === "string" &&
    typeof r.opcion === "string" &&
    typeof r.opcion_ganadora === "string" &&
    typeof r.gano === "boolean" &&
    typeof r.puntos_obtenidos === "number" &&
    typeof r.puntaje_total === "number"
  );
}

// Al llegar mercado_liquidado. El motor envia todos los resultado_apuesta de
// un mercado ANTES de su mercado_liquidado; si para ese mercado no llego
// ninguno, el jugador no aposto en el.
export function alLiquidarse(vista: VistaResultado, mercadoId: unknown): VistaResultado {
  if (vista.tipo === "resultado" && vista.resultado.mercado_id === mercadoId) return vista;
  return { tipo: "sin_apuesta" };
}

// Los puntos son multiplos de 0,5 (10 + monto x 0,5): con un decimal se
// muestran exactos, sin redondear 12,5 a 13.
const formato = new Intl.NumberFormat("es-CO", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

export function formatoPuntos(puntos: number): string {
  return formato.format(puntos);
}
