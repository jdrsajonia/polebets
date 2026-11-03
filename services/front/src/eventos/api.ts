// Datos del calendario de eventos (HU2-05). El front solo conoce estas
// funciones y tipos; las llamadas van al Gateway -> Orquestador (/api/eventos).

import { http } from "../api/http";

// Un evento del calendario (GET /api/eventos).
export interface Evento {
  evento_id: string;
  carrera: string;
  estado: "programado" | "en_curso" | "finalizado";
  estado_torneo: "abierto" | "activo" | "cancelado" | "repartido";
  largada: string; // ISO 8601
  inscritos: number;
  botin: number;
  costo_inscripcion: number;
  fichas_iniciales: number;
}

export interface Opcion {
  opcion: string;
  multiplicador: number;
}

export interface MercadoEstatico {
  mercado_id: string;
  plantilla: string;
  pregunta: string;
  estado: "programado" | "abierto" | "cerrado" | "liquidado";
  cierra_en: string | null;
  opciones: Opcion[];
}

export interface MiInscripcion {
  inscrito: boolean;
  devuelta: boolean;
}

// Detalle de un evento (GET /api/eventos/{id}).
export interface DetalleEvento {
  evento: Evento;
  mercados_estaticos: MercadoEstatico[];
  mi_inscripcion: MiInscripcion;
}

interface RespuestaLista {
  eventos: Evento[];
}

export function listarEventos(signal?: AbortSignal): Promise<RespuestaLista> {
  return http.get<RespuestaLista>("api", "/eventos", { signal });
}

export function obtenerEvento(eventoID: string, signal?: AbortSignal): Promise<DetalleEvento> {
  return http.get<DetalleEvento>("api", `/eventos/${encodeURIComponent(eventoID)}`, { signal });
}

// POST /api/eventos/{id}/inscripcion (orquestador-api.md §3.5, HU2-06). Sin
// cuerpo y sin Idempotency-Key: la clave la arma el Orquestador con
// (evento, usuario), asi que un reintento o un segundo clic no cobran dos
// veces. 201 inscrito ahora; 200 ya estaba inscrito (ya_inscrito, sin cobro).
export interface RespuestaInscripcion {
  evento_id: string;
  ya_inscrito: boolean;
  costo_inscripcion: number;
  fichas_iniciales: number;
  botin: number;
  saldo: number | null;
}

export function inscribirme(eventoID: string): Promise<RespuestaInscripcion> {
  return http.post<RespuestaInscripcion>("api", `/eventos/${encodeURIComponent(eventoID)}/inscripcion`);
}
