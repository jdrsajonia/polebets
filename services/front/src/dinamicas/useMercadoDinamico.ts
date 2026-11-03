// Hook de HU2-08: el mercado dinamico actual, al dia por WebSocket.
//
//   const mercado = useMercadoDinamico(); // MensajeDinamico | null
//
// Escucha solo "mercado_dinamico"; no toca mercado_abierto/cerrado/liquidado
// del P1 (useMercado), que conviven en la misma pantalla. No hay ruta REST
// del mercado dinamico actual: quien entra o recarga con uno ya abierto lo ve
// en a lo sumo 5 s, porque el motor reenvia el abierto cada 5 s. Ese reenvio
// llega igual al que ya se tiene y no cambia nada (siguienteMercado conserva
// el mismo market_id). El cierre o el resultado de uno que no se vio abierto
// se ignoran.

import { useEffect, useState } from "react";
import { TIPOS_WS, type MensajeDinamico } from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { esMensajeDinamico, siguienteMercado } from "./dinamico";

export function useMercadoDinamico(): MensajeDinamico | null {
  const { suscribir } = useWs();
  const [mercado, setMercado] = useState<MensajeDinamico | null>(null);

  useEffect(
    () =>
      suscribir<unknown>(TIPOS_WS.mercadoDinamico, (payload) => {
        if (esMensajeDinamico(payload)) setMercado((actual) => siguienteMercado(actual, payload));
      }),
    [suscribir],
  );

  return mercado;
}
