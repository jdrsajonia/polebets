// Cuenta regresiva hasta la hora de largada (HU2-05). El cliente solo muestra
// el tiempo que falta; quien abre y cierra los mercados es el servidor por el
// tiempo de carrera (RNF-19). Si la largada ya pasó, muestra "En carrera".

import { useEffect, useState } from "react";

function restanteMs(largadaISO: string): number {
  return new Date(largadaISO).getTime() - Date.now();
}

// "1h 05m 09s" / "05m 09s" / "09s".
function formato(ms: number): string {
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const dd = (n: number) => String(n).padStart(2, "0");
  if (h > 0) return `${h}h ${dd(m)}m ${dd(s)}s`;
  if (m > 0) return `${m}m ${dd(s)}s`;
  return `${s}s`;
}

interface Props {
  largada: string;
}

export default function CuentaRegresiva({ largada }: Props) {
  const [ms, setMs] = useState(() => restanteMs(largada));

  useEffect(() => {
    setMs(restanteMs(largada));
    const id = setInterval(() => setMs(restanteMs(largada)), 1000);
    return () => clearInterval(id);
  }, [largada]);

  if (ms <= 0) {
    return <span aria-live="polite">En carrera</span>;
  }
  return (
    <span aria-live="polite">
      Largada en <b>{formato(ms)}</b>
    </span>
  );
}
