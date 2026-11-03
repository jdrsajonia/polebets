// Modo previsto del jugador en un evento (RF-09). Se pide una vez por evento.

import { useEffect, useState } from "react";
import type { ModoApuesta } from "../api/tipos";
import { obtenerEvento } from "./api";
import { modoPrevisto } from "./modo";

export function useModoEvento(eventoId: string | null): ModoApuesta | null {
  const [modo, setModo] = useState<ModoApuesta | null>(null);

  useEffect(() => {
    setModo(null);
    if (!eventoId) return;
    const control = new AbortController();
    obtenerEvento(eventoId, control.signal)
      .then((d) => setModo(modoPrevisto(d)))
      .catch(() => {}); // cancelada al cambiar de evento
    return () => control.abort();
  }, [eventoId]);

  return modo;
}
