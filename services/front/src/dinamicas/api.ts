// REST de HU2-08 contra el Orquestador (por el API Gateway, /api).

import { http } from "../api/http";
import type { DetalleEvento, PeticionApuesta, RespuestaApuesta } from "../api/tipos";

// GET /api/eventos/{evento_id} (§3.4). Punto de conexion con HU2-05: si la
// ruta aun no existe o falla, devuelve null y el panel dice "el modo se
// confirma al apostar". Cuando HU2-05 la publique no hay que cambiar nada.
export async function obtenerEvento(eventoId: string, signal?: AbortSignal): Promise<DetalleEvento | null> {
  try {
    const d = await http.get<DetalleEvento>("api", `/eventos/${encodeURIComponent(eventoId)}`, { signal });
    return d && d.evento && d.mi_inscripcion ? d : null;
  } catch (causa) {
    if (causa instanceof DOMException && causa.name === "AbortError") throw causa;
    return null;
  }
}

// POST /api/apuestas (§3.6) con la Idempotency-Key del intento. 201 nueva,
// 200 reenvio con la misma clave (no se debita de nuevo). Lanza ErrorHttp con
// el codigo del contrato si falla.
export function apostar(peticion: PeticionApuesta, clave: string): Promise<RespuestaApuesta> {
  return http.post<RespuestaApuesta>("api", "/apuestas", peticion, { headers: { "Idempotency-Key": clave } });
}
