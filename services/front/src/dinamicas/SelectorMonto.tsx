// Monto de la apuesta con botones - y + (mockup "Carrera en vivo").

import { ajustarMonto, PASO_MONTO } from "./dinamico";
import estilos from "./PanelDinamico.module.css";

interface Props {
  valor: string; // texto del campo; se valida afuera (validarMonto)
  onCambio: (valor: string) => void;
  deshabilitado?: boolean;
}

export default function SelectorMonto({ valor, onCambio, deshabilitado = false }: Props) {
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
      <label className="solo-lectores" htmlFor="monto-dinamico">
        Monto
      </label>
      <input
        id="monto-dinamico"
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
