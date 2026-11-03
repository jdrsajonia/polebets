// Logica de POST /api/apuestas (orquestador-api.md §3.6), sin React.

import { ErrorHttp } from "../api/http";
import type { PeticionApuesta } from "../api/tipos";

// ---------------------------------------------------------------------------
// Idempotency-Key: UNA por intento (HU2-09 pide no repetir el bug de generar
// una por clic). Un intento es (mercado, opcion, monto): mientras no cambie,
// los reintentos tras un error o una red caida reusan la clave y el
// Orquestador/Motor devuelven la misma apuesta sin debitar dos veces. Se
// renueva solo tras un exito (olvidarla) o si cambia el intento.
// ---------------------------------------------------------------------------

export interface ClaveIntento {
  clave: string;
  intento: PeticionApuesta;
}

function mismoIntento(a: PeticionApuesta, b: PeticionApuesta): boolean {
  return a.mercado_id === b.mercado_id && a.opcion === b.opcion && a.monto === b.monto;
}

export function claveParaIntento(
  previa: ClaveIntento | null,
  intento: PeticionApuesta,
  nueva: () => string = () => crypto.randomUUID(),
): ClaveIntento {
  if (previa && mismoIntento(previa.intento, intento)) return previa;
  return { clave: nueva(), intento: { ...intento } };
}

// ---------------------------------------------------------------------------
// Errores del contrato con mensajes claros
// ---------------------------------------------------------------------------

const MENSAJES: Record<string, string> = {
  mercado_cerrado: "El mercado ya cerró: tu apuesta no entró.",
  ya_apostaste: "Ya apostaste en este mercado.",
  saldo_insuficiente: "No te alcanza el saldo de tokens para ese monto.",
  fichas_insuficientes: "No te alcanzan las fichas del torneo para ese monto.",
  monto_invalido: "Monto no válido: debe ser un entero mayor que 0 y no pasar el máximo por apuesta.",
  opcion_invalida: "Esa opción no es de este mercado.",
  mercado_no_existe: "El mercado ya no existe.",
  servicio_no_disponible: "El servicio no responde. Intenta de nuevo: la apuesta no se duplica.",
};

export function mensajeErrorApuesta(causa: unknown): string {
  if (causa instanceof ErrorHttp) {
    if (causa.codigo && MENSAJES[causa.codigo]) return MENSAJES[causa.codigo];
    if (causa.status === 503) return MENSAJES.servicio_no_disponible;
    if (causa.status === 0) return "No se pudo conectar. Intenta de nuevo: la apuesta no se duplica.";
    return causa.message;
  }
  return "No se pudo registrar la apuesta.";
}

// Errores tras los cuales no tiene sentido volver a intentar en este mercado:
// el boton queda deshabilitado.
export function bloqueaElMercado(causa: unknown): boolean {
  return causa instanceof ErrorHttp && (causa.codigo === "mercado_cerrado" || causa.codigo === "ya_apostaste");
}
