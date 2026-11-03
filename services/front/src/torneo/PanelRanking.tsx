// Ranking del torneo en /carrera (RF-18, front de HU2-11): puesto, jugador y
// fichas de cada inscrito (resaltando al actual), el botin y, para el jugador
// actual, la barra de avance hacia el minimo apostado para entrar al reparto.
// Solo para un inscrito con torneo activo (ranking.ts, mostrarRanking).

import { useAuth } from "../auth/AuthContext";
import { useModoEvento } from "../dinamicas/useModoEvento";
import estilos from "./PanelRanking.module.css";
import { anchoBarra, etiquetas, mostrarRanking } from "./ranking";
import { useRankingTorneo } from "./useRankingTorneo";

const numero = new Intl.NumberFormat("es-CO");

export default function PanelRanking() {
  const ranking = useRankingTorneo();
  const modo = useModoEvento(ranking?.evento_id ?? null);
  const { usuario } = useAuth();
  const yo = usuario?.id ?? null;

  if (!ranking || !mostrarRanking(ranking, modo, yo)) return null;

  const nombres = etiquetas(ranking.filas, yo);
  const mia = ranking.filas.find((f) => f.usuario_id === yo) ?? null;

  return (
    <section className="tarjeta" aria-labelledby="titulo-ranking" data-testid="ranking-torneo">
      <div className={estilos.cabecera}>
        <h2 id="titulo-ranking" className="titulo-seccion">
          Ranking del torneo
        </h2>
        <span className="texto-secundario" data-testid="botin">
          Botín: <b>{numero.format(ranking.botin)} PoleCoins</b>
        </span>
      </div>

      {mia && (
        <div className={estilos.avance} data-testid="avance">
          <div className={estilos.avanceTexto}>
            <span>
              Apostado: <b>{numero.format(mia.apostado)}</b> de {numero.format(ranking.min_apostado)} fichas
            </span>
            {mia.elegible ? (
              <span className={estilos.elegible}>Elegible</span>
            ) : (
              <span className="texto-secundario">{Math.floor(anchoBarra(mia.avance_pct))} %</span>
            )}
          </div>
          <div
            className={estilos.barra}
            role="progressbar"
            aria-label="Avance hacia el mínimo apostado"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.floor(anchoBarra(mia.avance_pct))}
          >
            <span className={mia.elegible ? `${estilos.relleno} ${estilos.rellenoOk}` : estilos.relleno} style={{ width: `${anchoBarra(mia.avance_pct)}%` }} />
          </div>
        </div>
      )}

      <table className={estilos.tabla}>
        <thead>
          <tr>
            <th scope="col">#</th>
            <th scope="col">Jugador</th>
            <th scope="col" className={estilos.num}>
              Fichas
            </th>
          </tr>
        </thead>
        <tbody>
          {ranking.filas.map((f) => (
            <tr key={f.usuario_id} className={f.usuario_id === yo ? estilos.yo : undefined} aria-current={f.usuario_id === yo ? "true" : undefined}>
              <td className={estilos.puesto}>{f.puesto}</td>
              <td>{nombres.get(f.usuario_id)}</td>
              <td className={estilos.num}>{numero.format(f.fichas)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
