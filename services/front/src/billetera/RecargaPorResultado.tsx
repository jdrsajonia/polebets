// Recarga el saldo al llegar "resultado_apuesta" por WebSocket, en cualquier
// pantalla, para que los PoleCoins del header se actualicen sin recargar la
// pagina (HU2-10 paga y avisa al jugador).
//
// Acepta cualquier payload: el del P1 (HU-08, con puntos) y el de
// internal/liquidar (HU2-10, con pago y saldo). Lo que muestra cada uno lo
// sigue decidiendo PanelResultado; aqui solo se pide el saldo a Pagos, que es
// quien lo sabe.

import { useEffect } from "react";
import { TIPOS_WS } from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { useAuth } from "../auth/AuthContext";
import { useBilletera } from "./BilleteraContexto";

export default function RecargaPorResultado() {
  const { suscribir } = useWs();
  const { recargar } = useBilletera();
  const { usuario } = useAuth();
  const esJugador = usuario?.rol === "player";

  useEffect(() => {
    if (!esJugador) return;
    return suscribir<unknown>(TIPOS_WS.resultadoApuesta, () => recargar());
  }, [suscribir, recargar, esJugador]);

  return null;
}
