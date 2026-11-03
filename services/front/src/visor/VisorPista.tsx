// Tarjeta "Visor de pista" de /carrera (HU2-15, mockup 07): incrusta el
// elemento <visor-pista> de Blazor. El visor abre su propio WebSocket y dibuja
// solo; aqui solo se le dice a quien seguir y si muestra los codigos.

import { useEffect, useState } from "react";
import { MOSTRAR_LATENCIA } from "../api/config";
import { cargarVisor } from "./cargarVisor";
import estilos from "./VisorPista.module.css";

interface Props {
  seguido: number | null; // numero del piloto a resaltar
  cargar?: () => Promise<void>; // reemplaza la carga real en las pruebas
}

type Fase = "cargando" | "listo" | "error";

export default function VisorPista({ seguido, cargar = cargarVisor }: Props) {
  const [fase, setFase] = useState<Fase>("cargando");
  const [error, setError] = useState<string | null>(null);
  const [codigos, setCodigos] = useState(true);
  const [intento, setIntento] = useState(0);

  // Carga el runtime de Blazor (una vez por app) al montar o al reintentar.
  useEffect(() => {
    let vigente = true;
    setFase("cargando");
    cargar().then(
      () => vigente && setFase("listo"),
      (e: unknown) => {
        if (!vigente) return;
        setError(e instanceof Error ? e.message : "No se pudo cargar el visor");
        setFase("error");
      },
    );
    return () => {
      vigente = false;
    };
  }, [cargar, intento]);

  return (
    <section className={`tarjeta ${estilos.tarjeta}`} aria-labelledby="titulo-visor">
      <div className={estilos.cabecera}>
        <h2 id="titulo-visor" className="titulo-seccion" style={{ margin: 0 }}>
          Visor de pista
        </h2>
        <button
          type="button"
          className={`boton boton-secundario ${estilos.botonCodigos}`}
          aria-pressed={codigos}
          onClick={() => setCodigos((c) => !c)}
        >
          Mostrar códigos
        </button>
      </div>

      <div className={estilos.lienzo}>
        {fase === "listo" && (
          <visor-pista
            seguir={String(seguido ?? 0)}
            mostrar-codigos={codigos ? "true" : "false"}
            mostrar-fps={MOSTRAR_LATENCIA ? "true" : "false"}
          />
        )}
        {fase === "cargando" && (
          <p className={`texto-secundario ${estilos.aviso}`} role="status">
            Cargando el visor…
          </p>
        )}
        {fase === "error" && (
          <div role="alert" className={estilos.aviso}>
            <p style={{ margin: 0 }}>Visor no disponible: {error}</p>
            <button type="button" className="boton boton-secundario" onClick={() => setIntento((i) => i + 1)}>
              Reintentar
            </button>
          </div>
        )}
      </div>

      <div className={`texto-secundario ${estilos.pie}`}>
        <span>Vista superior · posiciones x/y de OpenF1, suavizadas entre muestras</span>
        <span>Toca un piloto en la tabla para seguirlo</span>
      </div>
    </section>
  );
}
