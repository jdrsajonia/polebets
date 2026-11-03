// Peticiones REST de HU-05.

import { ErrorHttp, http } from "../api/http";
import type { FotoPosiciones } from "../api/tipos";
import { esFoto } from "./foto";

// Pide al Orquestador la ultima foto de posiciones (GET /api/posiciones, que
// el Orquestador lee del Motor). Devuelve null si no hay una carrera en curso
// (404 o respuesta sin pilotos).
//
// Antes se pedia directo al Motor (/api/motor/posiciones), pero el Gateway no
// enruta REST del Motor: la peticion caia en el Orquestador y daba 404, asi
// que la tabla no aparecia hasta el primer frame del WebSocket (~5 s). Ahora
// entra por el mismo camino que el resto del REST del jugador.
export async function obtenerPosiciones(signal?: AbortSignal): Promise<FotoPosiciones | null> {
  try {
    const foto = await http.get<unknown>("api", "/posiciones", { signal });
    return esFoto(foto) && foto.pilotos.length > 0 ? foto : null;
  } catch (error) {
    if (error instanceof ErrorHttp && error.status === 404) return null;
    throw error;
  }
}
