// Inscripcion al torneo en el detalle del evento (front de HU2-06, RF-07).
//
// Boton "Inscribirme al torneo (N PoleCoins)" solo si el evento no largo, el
// torneo esta abierto y el jugador no esta inscrito. Pide una confirmacion
// corta antes de cobrar. La idempotencia la da el Orquestador (clave por
// evento y usuario): reintentar tras un error no cobra dos veces. Al
// inscribirse recarga el detalle (inscritos, botin) y el saldo del header.

import { useState } from "react";
import { useBilletera } from "../billetera/BilleteraContexto";
import { inscribirme, type DetalleEvento } from "./api";
import estilos from "./InscripcionTorneo.module.css";
import {
  bloqueaInscripcion,
  mensajeErrorInscripcion,
  TEXTO_CANCELADO,
  TEXTO_DEVUELTA,
  TEXTO_YA_INSCRITO,
  textoBoton,
  textoConfirmacion,
  textoInscrito,
  vistaInscripcion,
} from "./inscripcion";

interface Props {
  detalle: DetalleEvento;
  onInscrito: () => void; // recarga el detalle sin desmontar esta tarjeta
}

export default function InscripcionTorneo({ detalle, onInscrito }: Props) {
  const { recargar: recargarSaldo } = useBilletera();
  const [confirmando, setConfirmando] = useState(false);
  const [enviando, setEnviando] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bloqueado, setBloqueado] = useState(false);
  const [aviso, setAviso] = useState<string | null>(null); // "ya estabas inscrito"

  const { evento } = detalle;
  const vista = vistaInscripcion(detalle);

  async function confirmar() {
    setEnviando(true);
    setError(null);
    try {
      const r = await inscribirme(evento.evento_id);
      setConfirmando(false);
      if (r.ya_inscrito) setAviso(TEXTO_YA_INSCRITO);
      recargarSaldo();
      onInscrito();
    } catch (causa) {
      setError(mensajeErrorInscripcion(causa, evento.costo_inscripcion));
      if (bloqueaInscripcion(causa)) {
        setBloqueado(true);
        setConfirmando(false);
      }
    } finally {
      setEnviando(false);
    }
  }

  let contenido;
  if (vista === "inscrito") {
    contenido = (
      <p className={estilos.ok} role="status" data-testid="inscripcion-estado">
        {textoInscrito(evento.fichas_iniciales)}
      </p>
    );
  } else if (vista === "devuelta" || vista === "cancelado") {
    contenido = (
      <p className={estilos.aviso} role="status" data-testid="inscripcion-estado">
        {vista === "devuelta" ? TEXTO_DEVUELTA : TEXTO_CANCELADO}
      </p>
    );
  } else if (vista === "inscribible" && !bloqueado) {
    contenido = confirmando ? (
      <div className={estilos.confirmacion} role="group" aria-label="Confirmar inscripción">
        <p>{textoConfirmacion(evento.costo_inscripcion, evento.fichas_iniciales)}</p>
        <div className={estilos.acciones}>
          <button type="button" className="boton" onClick={() => void confirmar()} disabled={enviando}>
            {enviando ? "Inscribiendo…" : "Sí, inscribirme"}
          </button>
          <button type="button" className="boton boton-secundario" onClick={() => setConfirmando(false)} disabled={enviando}>
            Cancelar
          </button>
        </div>
      </div>
    ) : (
      <button type="button" className="boton" onClick={() => setConfirmando(true)}>
        {textoBoton(evento.costo_inscripcion)}
      </button>
    );
  } else {
    contenido = null; // ya largo o el torneo no esta abierto
  }

  if (!contenido && !error) return null;

  return (
    <div className={estilos.tarjeta} data-testid="inscripcion">
      {contenido}
      {aviso && <p className="texto-secundario">{aviso}</p>}
      {error && (
        <p className={estilos.error} role="alert" data-testid="inscripcion-error">
          {error}
        </p>
      )}
    </div>
  );
}
