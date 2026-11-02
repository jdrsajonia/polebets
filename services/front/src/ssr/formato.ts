// Formatos de la pagina SSR. Siempre con zona horaria y locale fijos: el
// servidor (Node) y el navegador deben producir EXACTAMENTE el mismo texto,
// o React avisa de que la hidratacion no coincide.

const ZONA = "America/Bogota";

const numero = new Intl.NumberFormat("es-CO");
const fechaHora = new Intl.DateTimeFormat("es-CO", {
  timeZone: ZONA,
  day: "2-digit",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

export function formatoNumero(n: number): string {
  return numero.format(n);
}

// "+50" / "−20": el signo siempre visible en los movimientos de tokens.
export function formatoDelta(n: number): string {
  return `${n > 0 ? "+" : n < 0 ? "−" : ""}${numero.format(Math.abs(n))}`;
}

export function formatoFecha(iso: string): string {
  const fecha = new Date(iso);
  return Number.isNaN(fecha.getTime()) ? "—" : fechaHora.format(fecha);
}

const TIPOS: Record<string, string> = {
  purchase: "Compra de tokens",
  debit: "Apuesta",
  settle: "Pago de apuesta",
  buy_in: "Inscripción a torneo",
  refund: "Devolución",
  prize: "Premio del torneo",
};

export function nombreTransaccion(tipo: string): string {
  return TIPOS[tipo] ?? tipo;
}

const ESTADOS: Record<string, string> = { programado: "Programado", en_curso: "En curso", finalizado: "Finalizado" };
const TORNEOS: Record<string, string> = {
  abierto: "Inscripciones abiertas",
  activo: "Torneo en juego",
  cancelado: "Cancelado",
  repartido: "Premios repartidos",
};

export function nombreEstado(estado: string): string {
  return ESTADOS[estado] ?? estado;
}

export function nombreTorneo(estado: string, devuelta: boolean): string {
  return devuelta ? "Cancelado: inscripción devuelta" : (TORNEOS[estado] ?? estado);
}
