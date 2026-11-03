// Logica de la apuesta estatica del detalle del evento (front de HU2-09,
// RF-09/RF-12), sin React. Reusa la del panel dinamico (dinamicas/): monto,
// pago potencial truncado y clave de idempotencia por intento.

import { pagoSiAcierta, validarMonto } from "../dinamicas/dinamico";
import type { MercadoEstatico } from "./api";

// Tope por apuesta individual (orquestador-api.md §2, monto_invalido). Las
// estaticas siempre son individuales, en PoleCoins.
export const MAXIMO_INDIVIDUAL = 500;

const numero = new Intl.NumberFormat("es-CO");

// Mismo formato de multiplicador que el calendario (×3,0).
export const formatoMult = new Intl.NumberFormat("es-CO", { minimumFractionDigits: 1, maximumFractionDigits: 2 });

export function validarMontoEstatico(texto: string): { ok: true; monto: number } | { ok: false; error: string } {
  const v = validarMonto(texto);
  if (v.ok && v.monto > MAXIMO_INDIVIDUAL) {
    return { ok: false, error: `El máximo por apuesta es ${numero.format(MAXIMO_INDIVIDUAL)} PoleCoins` };
  }
  return v;
}

// Se puede apostar solo con el mercado abierto y antes de su cierre (la
// largada). El reloj del front es solo una guia: el Motor rechaza lo tarde.
export function estaAbierto(m: MercadoEstatico, ahora: number = Date.now()): boolean {
  if (m.estado !== "abierto") return false;
  return m.cierra_en === null || Date.parse(m.cierra_en) > ahora;
}

// Lo que se ve ANTES de confirmar (RF-09).
export function textoAntesDeConfirmar(opcion: string, multiplicador: number, monto: number): string {
  return `Modo individual · «${opcion}» × ${formatoMult.format(multiplicador)}: si aciertas recibes ${numero.format(pagoSiAcierta(monto, multiplicador))} PoleCoins.`;
}

export const TEXTO_CERRADO = "Cerrado en la largada";
export const TEXTO_AUN_NO_ABRE = "Las apuestas de este mercado aún no abren.";
