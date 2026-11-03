// Resultado de la apuesta dinamica (HU2-13, RF-17), sin React.
//
// El panel solo da por resuelta la apuesta cuando llega el "resultado_apuesta"
// del servidor (worker de liquidar del motor): winning_option dice que opcion
// gano el mercado, pero el pago, el saldo y el puesto los confirma Pagos.
// El payload del P1 (con puntos) no pasa este validador ni al reves.

import type { MiApuesta, ResultadoDinamico } from "../api/tipos";

const numero = new Intl.NumberFormat("es-CO");

const esNumero = (v: unknown) => typeof v === "number" && Number.isFinite(v);

export function esResultadoDinamico(x: unknown): x is ResultadoDinamico {
  if (typeof x !== "object" || x === null) return false;
  const r = x as Record<string, unknown>;
  const base =
    typeof r.mercado_id === "string" &&
    typeof r.apuesta_id === "string" &&
    typeof r.opcion === "string" &&
    typeof r.opcion_ganadora === "string" &&
    typeof r.gano === "boolean" &&
    esNumero(r.monto) &&
    esNumero(r.multiplicador);
  if (!base) return false;
  if (r.modo === "individual") return esNumero(r.pago) && esNumero(r.saldo);
  if (r.modo === "torneo") {
    return esNumero(r.fichas_ganadas) && esNumero(r.fichas_perdidas) && esNumero(r.fichas_total) && esNumero(r.puesto);
  }
  return false;
}

export function textoResultado(r: ResultadoDinamico): string {
  if (r.modo === "torneo") {
    return r.gano
      ? `¡+${numero.format(r.fichas_ganadas)} fichas! Total: ${numero.format(r.fichas_total)} · Puesto #${r.puesto}`
      : `Perdiste ${numero.format(r.fichas_perdidas)} fichas · Puesto #${r.puesto}`;
  }
  return r.gano
    ? `¡Ganaste ${numero.format(r.pago)} PoleCoins! Saldo: ${numero.format(r.saldo)}`
    : `Perdiste ${numero.format(r.monto)} PoleCoins`;
}

// La apuesta ya liquidada segun mi-apuesta (status won o lost): el mismo texto
// que textoResultado con lo que hay; Pagos no da saldo, total ni puesto aqui.
export function estaLiquidada(a: MiApuesta): boolean {
  return a.status !== "pending";
}

export function textoLiquidada(a: MiApuesta): string {
  const gano = a.status === "won";
  const pago = numero.format(a.payout ?? 0);
  if (a.modo === "torneo") return gano ? `¡+${pago} fichas!` : `Perdiste ${numero.format(a.monto)} fichas`;
  return gano ? `¡Ganaste ${pago} PoleCoins!` : `Perdiste ${numero.format(a.monto)} PoleCoins`;
}
