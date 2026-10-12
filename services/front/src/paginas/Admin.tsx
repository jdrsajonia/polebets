// Pantalla /admin: herramienta PROVISIONAL del prototipo para abrir mercados
// (HU-11/12). En la app final las preguntas las define el sistema y esta
// pantalla no existe; por eso va fuera del Layout, en HTML plano y sin el
// diseño de PoleBet, para que no se confunda con una pantalla del producto.
//
// La logica es la de siempre: el mercado actual llega por REST y WebSocket
// (useMercado), la cuenta regresiva usa el reloj del servidor (RNF-05) y no se
// puede abrir otro mientras haya uno abierto (el motor tambien responde 409).

import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { ErrorHttp } from "../api/http";
import type { Mercado } from "../api/tipos";
import { useAuth } from "../auth/AuthContext";
import { abrirMercado } from "../mercados/api";
import { faseDe, formatoReloj, OPCIONES_SI_NO, PREGUNTA_CAMBIO_POSICION, segundosHasta, validarPeticion } from "../mercados/mercado";
import { useMercado, useRelojServidor } from "../mercados/useMercado";
import estilos from "./Admin.module.css";

interface Mensaje {
  ok: boolean;
  texto: string;
}

// Pregunta y opciones fijas del unico tipo de mercado (Si/No).
const PREGUNTA = PREGUNTA_CAMBIO_POSICION;
const OPCIONES = [...OPCIONES_SI_NO];

// Estado del mercado en una linea, con la cuenta regresiva que toque.
function estadoEnTexto(m: Mercado, ahora: number): string {
  switch (faseDe(m, ahora)) {
    case "abierto":
      return `abierto · cierra en ${formatoReloj(segundosHasta(m.cierra_en, ahora))}`;
    case "observacion":
      return `cerrado, en observación · se liquida en ${formatoReloj(segundosHasta(m.observa_hasta, ahora))}`;
    case "liquidando":
      return "cerrado, liquidando…";
    case "liquidado":
      return "liquidado";
  }
}

export default function Admin() {
  const { mercado, cargando, error, desfaseMs, aplicar, recargar } = useMercado();
  const ahora = useRelojServidor(desfaseMs);
  const { cerrarSesion } = useAuth();
  const navegar = useNavigate();

  const [enviando, setEnviando] = useState(false);
  const [mensaje, setMensaje] = useState<Mensaje | null>(null);

  const hayAbierto = mercado !== null && faseDe(mercado, ahora) === "abierto";
  // Se sigue validando con las mismas reglas del motor por si las constantes
  // cambian algun dia; hoy siempre es valida.
  const validacion = validarPeticion(PREGUNTA, OPCIONES);
  const puedeAbrir = validacion.ok && !enviando && !hayAbierto;

  async function onSubmit(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (!puedeAbrir || !validacion.ok) return; // tambien evita el doble envio
    setEnviando(true);
    setMensaje(null);
    try {
      const nuevo = await abrirMercado(validacion.peticion);
      aplicar(nuevo); // el mensaje del WebSocket llega tambien; reemplaza() evita duplicar
      setMensaje({ ok: true, texto: "Mercado abierto. Los jugadores conectados ya lo están viendo." });
    } catch (causa) {
      setMensaje({ ok: false, texto: causa instanceof ErrorHttp ? causa.message : "Ocurrió un error inesperado." });
    } finally {
      setEnviando(false);
    }
  }

  async function salir() {
    await cerrarSesion();
    navegar("/login", { replace: true });
  }

  return (
    <div className={estilos.pagina}>
      <div className={estilos.contenido}>
        <h1>Admin (herramienta de prueba)</h1>
        <p>Pantalla provisional para abrir mercados durante el prototipo.</p>

        <h2>Mercado actual</h2>
        {error ? (
          <div role="alert">
            <p>{error}</p>
            <button type="button" onClick={recargar}>
              Reintentar
            </button>
          </div>
        ) : mercado ? (
          <>
            <p data-testid="estado-mercado">Estado: {estadoEnTexto(mercado, ahora)}</p>
            <p>{mercado.pregunta}</p>
          </>
        ) : (
          <p>{cargando ? "Cargando…" : "Ningún mercado abierto todavía."}</p>
        )}

        <h2>Abrir un mercado</h2>
        <form onSubmit={onSubmit} noValidate>
          <p>Pregunta: {PREGUNTA}</p>
          <p>Opciones: {OPCIONES.join(" / ")}</p>

          {!validacion.ok && <p>{validacion.error}</p>}
          {hayAbierto && <p>Ya hay un mercado abierto</p>}

          <button type="submit" disabled={!puedeAbrir}>
            {enviando ? "Abriendo…" : "Abrir mercado"}
          </button>

          <div aria-live="polite">
            {mensaje && <p role={mensaje.ok ? "status" : "alert"}>{mensaje.texto}</p>}
          </div>
        </form>

        <p>
          <button type="button" onClick={salir}>
            Salir
          </button>
        </p>
      </div>
    </div>
  );
}
