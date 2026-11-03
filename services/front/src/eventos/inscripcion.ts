// Logica de la inscripcion al torneo (front de HU2-06, RF-07), sin React.

import { ErrorHttp } from "../api/http";
import type { DetalleEvento, Evento } from "./api";

const numero = new Intl.NumberFormat("es-CO");

// Lo que muestra la tarjeta de inscripcion del detalle:
//   inscribible  -> boton "Inscribirme al torneo (N PoleCoins)"
//   inscrito     -> recibira las fichas en la largada (o ya las tiene)
//   devuelta     -> torneo cancelado (< 4 inscritos): se le devolvio
//   cancelado    -> torneo cancelado y no estaba inscrito
//   cerrada      -> ya largo o el torneo no esta abierto: nada que hacer
export type VistaInscripcion = "inscribible" | "inscrito" | "devuelta" | "cancelado" | "cerrada";

export function vistaInscripcion(d: DetalleEvento, ahora: number = Date.now()): VistaInscripcion {
  const { evento, mi_inscripcion: mia } = d;
  if (mia.inscrito) return mia.devuelta || evento.estado_torneo === "cancelado" ? "devuelta" : "inscrito";
  if (evento.estado_torneo === "cancelado") return "cancelado";
  const largo = evento.estado !== "programado" || Date.parse(evento.largada) <= ahora;
  return !largo && evento.estado_torneo === "abierto" ? "inscribible" : "cerrada";
}

export function textoBoton(costo: number): string {
  return `Inscribirme al torneo (${numero.format(costo)} PoleCoins)`;
}

export function textoConfirmacion(costo: number, fichas: number): string {
  return `Se descuentan ${numero.format(costo)} PoleCoins de tu saldo y recibirás ${numero.format(fichas)} fichas en la largada. Si al largar hay menos de 4 inscritos, el torneo se cancela y te devolvemos la inscripción.`;
}

// Antes de la largada: cuantas fichas recibira. Ya largado (no programado) y
// con el torneo activo: ya esta jugando.
export function textoInscrito(evento: Pick<Evento, "estado" | "estado_torneo" | "fichas_iniciales">): string {
  if (evento.estado !== "programado" && evento.estado_torneo === "activo") return "Inscrito · torneo en curso";
  return `Inscrito · recibirás ${numero.format(evento.fichas_iniciales)} fichas en la largada`;
}

export const TEXTO_DEVUELTA = "Torneo cancelado: te devolvimos la inscripción";
export const TEXTO_CANCELADO = "Torneo cancelado: no se juega en este evento";
export const TEXTO_YA_INSCRITO = "Ya estabas inscrito en este torneo: no se cobró de nuevo.";

export function mensajeErrorInscripcion(causa: unknown, costo: number): string {
  if (causa instanceof ErrorHttp) {
    switch (causa.codigo) {
      case "saldo_insuficiente":
        return `No te alcanza el saldo: la inscripción cuesta ${numero.format(costo)} PoleCoins.`;
      case "inscripciones_cerradas":
        return "Las inscripciones ya cerraron: la carrera largó.";
      case "evento_no_existe":
        return "El evento ya no existe.";
    }
    if (causa.status === 503 || causa.codigo === "servicio_no_disponible") {
      return "El servicio no responde. Intenta de nuevo: no se cobra dos veces.";
    }
    if (causa.status === 0) return "No se pudo conectar. Intenta de nuevo: no se cobra dos veces.";
    return causa.message;
  }
  return "No se pudo completar la inscripción.";
}

// Errores tras los cuales el boton ya no sirve.
export function bloqueaInscripcion(causa: unknown): boolean {
  return causa instanceof ErrorHttp && (causa.codigo === "inscripciones_cerradas" || causa.codigo === "evento_no_existe");
}
