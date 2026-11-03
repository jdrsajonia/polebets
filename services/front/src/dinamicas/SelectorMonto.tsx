// Monto de la apuesta con botones - y + (mockup "Carrera en vivo"). Lo usan
// el panel dinamico y la apuesta estatica del detalle del evento (HU2-09);
// con varios en la misma pantalla, cada uno lleva su propio id.

import { ajustarMonto, PASO_MONTO } from "./dinamico";
import estilos from "./PanelDinamico.module.css";

interface Props {
  valor: string; // texto del campo; se valida afuera (validarMonto)
  onCambio: (valor: string) => void;
  deshabilitado?: boolean;
  id?: string; // id del campo (y del for de su etiqueta)
}

export default function SelectorMonto({ valor, onCambio, deshabilitado = false, id = "monto-dinamico" }: Props) {
  return (
    <div className={estilos.monto}>
      <button
        type="button"
        className="boton boton-secundario"
        aria-label={`Restar ${PASO_MONTO}`}
        onClick={() => onCambio(ajustarMonto(valor, -PASO_MONTO))}
        disabled={deshabilitado}
      >
        −
      </button>
      <label className="solo-lectores" htmlFor={id}>
        Monto
      </label>
      <input
        id={id}
        className={estilos.campoMonto}
        inputMode="numeric"
        value={valor}
        onChange={(e) => onCambio(e.target.value)}
        disabled={deshabilitado}
      />
      <button
        type="button"
        className="boton boton-secundario"
        aria-label={`Sumar ${PASO_MONTO}`}
        onClick={() => onCambio(ajustarMonto(valor, PASO_MONTO))}
        disabled={deshabilitado}
      >
        +
      </button>
    </div>
  );
}
