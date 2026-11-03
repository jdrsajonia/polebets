// Pantalla /eventos (HU2-05): calendario de eventos. Muestra los eventos
// programados y en curso con su largada, inscritos y botín; al abrir uno, sus
// mercados estáticos con multiplicadores y la cuenta regresiva.
//
// Criterios que cubre:
//   - Entro al calendario y veo cada evento con hora de largada, inscritos y botín.
//   - Abro un evento y veo sus mercados estáticos (opciones con multiplicador)
//     y la cuenta regresiva hasta la largada.

import { useCallback, useEffect, useState } from "react";
import { ErrorHttp } from "../api/http";
import ApuestaEstatica from "../eventos/ApuestaEstatica";
import CuentaRegresiva from "../eventos/CuentaRegresiva";
import InscripcionTorneo from "../eventos/InscripcionTorneo";
import { listarEventos, obtenerEvento, type DetalleEvento, type Evento } from "../eventos/api";
import estilos from "./Calendario.module.css";

const numero = new Intl.NumberFormat("es-CO");

const ETIQUETA_ESTADO: Record<Evento["estado"], string> = {
  programado: "Programado",
  en_curso: "En curso",
  finalizado: "Finalizado",
};

function mensajeDeError(causa: unknown): string {
  return causa instanceof ErrorHttp ? causa.message : "Ocurrió un error inesperado.";
}

export default function Calendario() {
  const [eventos, setEventos] = useState<Evento[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [seleccionado, setSeleccionado] = useState<string | null>(null);

  const cargar = useCallback((signal?: AbortSignal) => {
    setError(null);
    setEventos(null);
    listarEventos(signal)
      .then((r) => setEventos(r.eventos))
      .catch((causa) => {
        if (signal?.aborted) return;
        setError(mensajeDeError(causa));
      });
  }, []);

  useEffect(() => {
    const ac = new AbortController();
    cargar(ac.signal);
    return () => ac.abort();
  }, [cargar]);

  if (seleccionado) {
    return <Detalle eventoID={seleccionado} onVolver={() => setSeleccionado(null)} />;
  }

  return (
    <div className={estilos.pagina}>
      <h1 className={estilos.titulo}>Calendario</h1>

      {error && (
        <div role="alert" className={`tarjeta ${estilos.error}`}>
          <p>{error}</p>
          <button type="button" className="boton boton-secundario" onClick={() => cargar()}>
            Reintentar
          </button>
        </div>
      )}

      {!error && eventos === null && <p className="texto-secundario">Cargando eventos…</p>}

      {!error && eventos !== null && eventos.length === 0 && (
        <p className="texto-secundario">No hay eventos programados por ahora.</p>
      )}

      {eventos !== null && eventos.length > 0 && (
        <ul className={estilos.lista}>
          {eventos.map((e) => (
            <li key={e.evento_id}>
              <button type="button" className={`tarjeta ${estilos.tarjetaEvento}`} onClick={() => setSeleccionado(e.evento_id)}>
                <div className={estilos.filaEvento}>
                  <span className={estilos.carrera}>{e.carrera}</span>
                  <span className={estilos.estado} data-estado={e.estado}>
                    {ETIQUETA_ESTADO[e.estado]}
                  </span>
                </div>
                <div className={estilos.metaEvento}>
                  <CuentaRegresiva largada={e.largada} />
                  <span>· {numero.format(e.inscritos)} inscritos</span>
                  <span>· Botín {numero.format(e.botin)} PoleCoins</span>
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

interface DetalleProps {
  eventoID: string;
  onVolver: () => void;
}

function Detalle({ eventoID, onVolver }: DetalleProps) {
  const [datos, setDatos] = useState<DetalleEvento | null>(null);
  const [error, setError] = useState<string | null>(null);

  const cargar = useCallback(
    (signal?: AbortSignal) => {
      setError(null);
      setDatos(null);
      obtenerEvento(eventoID, signal)
        .then(setDatos)
        .catch((causa) => {
          if (signal?.aborted) return;
          setError(mensajeDeError(causa));
        });
    },
    [eventoID],
  );

  useEffect(() => {
    const ac = new AbortController();
    cargar(ac.signal);
    return () => ac.abort();
  }, [cargar]);

  // HU2-06: tras inscribirse, vuelve a pedir el detalle (inscritos, botin,
  // mi_inscripcion) sin pasar por "Cargando…", para no desmontar la tarjeta.
  // Si falla, se queda lo que habia.
  const refrescar = useCallback(() => {
    obtenerEvento(eventoID)
      .then(setDatos)
      .catch(() => {});
  }, [eventoID]);

  return (
    <div className={estilos.pagina}>
      <button type="button" className="boton boton-secundario" onClick={onVolver}>
        ← Volver al calendario
      </button>

      {error && (
        <div role="alert" className={`tarjeta ${estilos.error}`}>
          <p>{error}</p>
          <button type="button" className="boton boton-secundario" onClick={() => cargar()}>
            Reintentar
          </button>
        </div>
      )}

      {!error && datos === null && <p className="texto-secundario">Cargando evento…</p>}

      {datos && (
        <>
          <section className={`tarjeta ${estilos.cabecera}`} aria-labelledby="titulo-evento">
            <h1 id="titulo-evento" className={estilos.titulo}>
              {datos.evento.carrera}
            </h1>
            <div className={estilos.metaEvento}>
              <CuentaRegresiva largada={datos.evento.largada} />
              <span>· {numero.format(datos.evento.inscritos)} inscritos</span>
              <span>· Botín {numero.format(datos.evento.botin)} PoleCoins</span>
            </div>
            {/* HU2-06: inscripcion al torneo, o si ya esta inscrito o se cancelo */}
            <InscripcionTorneo detalle={datos} onInscrito={refrescar} />
          </section>

          <h2 className={estilos.subtitulo}>Apuestas estáticas</h2>
          {datos.mercados_estaticos.length === 0 ? (
            <p className="texto-secundario">Este evento todavía no tiene mercados estáticos.</p>
          ) : (
            <ul className={estilos.lista}>
              {datos.mercados_estaticos.map((m) => (
                <li key={m.mercado_id} className={`tarjeta ${estilos.mercado}`}>
                  <div className={estilos.filaEvento}>
                    <span className={estilos.pregunta}>{m.pregunta}</span>
                    <span className={estilos.estado} data-estado={m.estado}>
                      {m.estado}
                    </span>
                  </div>
                  {/* HU2-09: las opciones con su multiplicador son tambien la apuesta */}
                  <ApuestaEstatica mercado={m} />
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}
