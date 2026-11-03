// REST de HU2-08 contra el Orquestador (por el API Gateway, /api).

import { ErrorHttp, http } from "../api/http";
import type { DetalleEvento, MiApuesta, PeticionApuesta, RespuestaApuesta } from "../api/tipos";

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

// GET /api/mercados/{mercado_id}/mi-apuesta (§3.7, HU2-13). null si el
// jugador no aposto (404 sin_apuesta) o el mercado no existe; otros errores
// se lanzan.
export async function obtenerMiApuesta(mercadoId: string, signal?: AbortSignal): Promise<MiApuesta | null> {
  try {
    return await http.get<MiApuesta>("api", `/mercados/${encodeURIComponent(mercadoId)}/mi-apuesta`, { signal });
  } catch (causa) {
    if (causa instanceof ErrorHttp && causa.status === 404) return null;
    throw causa;
  }
}
