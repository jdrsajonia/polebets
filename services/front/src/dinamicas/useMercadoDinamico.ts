// Hook de HU2-08: el mercado dinamico actual, al dia por WebSocket.
//
//   const mercado = useMercadoDinamico(); // MensajeDinamico | null
//
// Al montar pide la foto inicial por REST (GET /api/dinamica-actual, que el
// Orquestador lee del Motor), para que quien entra o recarga vea la apuesta en
// vivo de inmediato, sin esperar al reenvio del WebSocket (~5 s). Despues
// escucha solo "mercado_dinamico"; no toca mercado_abierto/cerrado/liquidado
// del P1 (useMercado), que conviven en la misma pantalla. La foto inicial y
// los mensajes del WebSocket se combinan con siguienteMercado: un mensaje mas
// nuevo del mismo mercado avanza de fase y uno viejo no retrocede. Con un
// mercado ya en pantalla, el cierre o el resultado de otro se ignoran.

import { useEffect, useState } from "react";
import { TIPOS_WS, type MensajeDinamico } from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { obtenerDinamicaActual } from "./api";
import { esMensajeDinamico, siguienteMercado } from "./dinamico";

export function useMercadoDinamico(): MensajeDinamico | null {
  const { suscribir } = useWs();
  const [mercado, setMercado] = useState<MensajeDinamico | null>(null);

  // Foto inicial por REST, una vez al montar.
  useEffect(() => {
    const ac = new AbortController();
    obtenerDinamicaActual(ac.signal)
      .then((inicial) => {
        if (inicial) setMercado((actual) => siguienteMercado(actual, inicial));
      })
      .catch(() => {
        /* sin foto inicial: el WebSocket la traera en a lo sumo 5 s */
      });
    return () => ac.abort();
  }, []);

  useEffect(
    () =>
      suscribir<unknown>(TIPOS_WS.mercadoDinamico, (payload) => {
        if (esMensajeDinamico(payload)) setMercado((actual) => siguienteMercado(actual, payload));
      }),
    [suscribir],
  );

  return mercado;
}
