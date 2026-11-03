// Logica del ranking del torneo en /carrera (RF-18, front de HU2-11), sin
// React para probarla sola.
//
// El motor difunde "ranking_torneo" a todos cuando cambian las fichas y lo
// reenvia cada 5 s mientras el torneo esta activo (internal/fichas). Trae
// usuario_id y no nombres: el jugador actual es "Tú" y el resto "Jugador N",
// numerados por usuario_id para que el numero no cambie al moverse la tabla
// (no se llama a Usuarios por esto).

import type { FilaRanking, ModoApuesta, RankingTorneo } from "../api/tipos";

const esNumero = (v: unknown) => typeof v === "number" && Number.isFinite(v);

function esFila(x: unknown): x is FilaRanking {
  if (typeof x !== "object" || x === null) return false;
  const f = x as Record<string, unknown>;
  return (
    esNumero(f.puesto) &&
    typeof f.usuario_id === "string" &&
    esNumero(f.fichas) &&
    esNumero(f.apostado) &&
    esNumero(f.avance_pct) &&
    typeof f.elegible === "boolean"
  );
}

export function esRankingTorneo(x: unknown): x is RankingTorneo {
  if (typeof x !== "object" || x === null) return false;
  const r = x as Record<string, unknown>;
  return (
    typeof r.evento_id === "string" &&
    Array.isArray(r.filas) &&
    r.filas.every(esFila) &&
    esNumero(r.min_apostado) &&
    esNumero(r.botin) &&
    typeof r.actualizado_en === "string"
  );
}

// Siguiente ranking en pantalla: el de otro evento lo reemplaza; del mismo,
// uno con actualizado_en anterior (llego tarde) se descarta.
export function siguienteRanking(actual: RankingTorneo | null, msg: RankingTorneo): RankingTorneo {
  if (!actual || actual.evento_id !== msg.evento_id) return msg;
  return Date.parse(msg.actualizado_en) < Date.parse(actual.actualizado_en) ? actual : msg;
}

// Nombre a mostrar por usuario_id: "Tú" y "Jugador 1..n".
export function etiquetas(filas: FilaRanking[], yo: string | null): Map<string, string> {
  const otros = filas.map((f) => f.usuario_id).filter((id) => id !== yo).sort();
  const m = new Map(otros.map((id, i) => [id, `Jugador ${i + 1}`]));
  if (yo) m.set(yo, "Tú");
  return m;
}

// Se muestra solo a un inscrito con torneo activo. El modo sale de
// GET /api/eventos/{id} (modo.ts); si esa ruta aun no responde (null), estar
// en el ranking ya lo dice: el motor solo tiene fichas de inscritos de un
// torneo que largo y no se cancelo.
export function mostrarRanking(ranking: RankingTorneo | null, modo: ModoApuesta | null, yo: string | null): boolean {
  if (!ranking || !yo) return false;
  if (modo === "torneo") return true;
  return modo === null && ranking.filas.some((f) => f.usuario_id === yo);
}

// La barra va de 0 a 100 (el motor ya la topa; aqui por si acaso).
export function anchoBarra(avancePct: number): number {
  return Math.min(100, Math.max(0, avancePct));
}
