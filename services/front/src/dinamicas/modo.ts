// Modo de la apuesta antes de confirmar (RF-09, orquestador-api.md §3.4).
//
// No hay ruta de vista previa: el front usa GET /api/eventos/{event_id} del
// Orquestador. En las dinamicas, inscrito (no devuelto) con torneo "activo" =
// torneo; si no, individual. Si la ruta aun no existe (HU2-05) o falla, el
// modo queda sin saber (null) y se confirma al apostar: quien decide es el
// motor (§3.6).

import type { DetalleEvento, ModoApuesta } from "../api/tipos";

export function modoPrevisto(detalle: DetalleEvento | null): ModoApuesta | null {
  if (!detalle) return null;
  const { inscrito, devuelta } = detalle.mi_inscripcion;
  return inscrito && !devuelta && detalle.evento.estado_torneo === "activo" ? "torneo" : "individual";
}

// Unidad del pago segun el modo.
export function unidad(modo: ModoApuesta | null): string {
  if (modo === "torneo") return "fichas";
  if (modo === "individual") return "tokens";
  return "tokens o fichas";
}

export const TEXTO_MODO: Record<ModoApuesta | "desconocido", string> = {
  individual: "Modo individual: apuestas tokens",
  torneo: "Modo torneo: apuestas fichas del torneo",
  desconocido: "El modo se confirma al apostar",
};
