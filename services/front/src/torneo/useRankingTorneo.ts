// Hook del ranking del torneo: el ultimo "ranking_torneo" valido.

import { useEffect, useState } from "react";
import { TIPOS_WS, type RankingTorneo } from "../api/tipos";
import { useWs } from "../api/WsContexto";
import { esRankingTorneo, siguienteRanking } from "./ranking";

export function useRankingTorneo(): RankingTorneo | null {
  const { suscribir } = useWs();
  const [ranking, setRanking] = useState<RankingTorneo | null>(null);

  useEffect(
    () =>
      suscribir<unknown>(TIPOS_WS.rankingTorneo, (payload) => {
        if (esRankingTorneo(payload)) setRanking((actual) => siguienteRanking(actual, payload));
      }),
    [suscribir],
  );

  return ranking;
}
