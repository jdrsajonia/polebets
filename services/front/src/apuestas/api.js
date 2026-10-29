// Logica de HU-06 en el front, sin React para poder probarla sola.
// El front nunca toca el saldo: solo le pide al motor que registre la apuesta.
//
// Las peticiones van por ruta relativa (/api/motor, de src/api/config.ts): el
// navegador habla con el mismo origen desde el que se sirvio el front y Nginx
// hace de proxy al motor (ver services/front/nginx.conf). Asi funciona igual en
// localhost, en la LAN y en la nube sin recompilar.
import { MOTOR_URL } from "../api/config";

// Regla provisional del prototipo: acertar suma 10 + (monto x 0,5) puntos.
// Esta en una sola funcion porque el grupo aun no cierra como funcionan los
// puntos; si cambia, se cambia solo aqui (y en motor: PuntosPotenciales).
export function puntosPotenciales(monto) {
  return 10 + monto * 0.5;
}

// Segundos que faltan para el cierre. Es solo informativo: el que decide si
// la apuesta entra es el reloj del servidor (RNF-05).
export function segundosRestantes(cierraEn, ahora = Date.now()) {
  const fin = new Date(cierraEn).getTime();
  if (Number.isNaN(fin)) return 0;
  return Math.max(0, Math.ceil((fin - ahora) / 1000));
}

export function formatoReloj(segundos) {
  const m = Math.floor(segundos / 60);
  const s = String(segundos % 60).padStart(2, "0");
  return `${m}:${s}`;
}

// Valida el texto del campo monto. Devuelve { ok, monto, error }.
export function validarMonto(texto, saldo = null) {
  const limpio = String(texto ?? "").trim();
  if (limpio === "") return { ok: false, monto: 0, error: "" };
  if (!/^\d+$/.test(limpio)) {
    return { ok: false, monto: 0, error: "El monto debe ser un numero entero mayor que 0" };
  }
  const monto = Number(limpio);
  if (monto <= 0) return { ok: false, monto: 0, error: "El monto debe ser mayor que 0" };
  if (saldo !== null && monto > saldo) {
    return { ok: false, monto, error: "Saldo insuficiente" };
  }
  return { ok: true, monto, error: "" };
}

// Envia la apuesta al motor (POST /apuestas). La cookie de sesion (JWT) viaja
// sola gracias a credentials: "include". Devuelve siempre un objeto:
//   { ok: true, apuesta }  |  { ok: false, status, mensaje }
export async function apostar({ mercadoId, opcion, monto, clave, fetchFn = fetch, baseUrl = MOTOR_URL }) {
  let resp;
  try {
    resp = await fetchFn(`${baseUrl}/apuestas`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", "Idempotency-Key": clave },
      body: JSON.stringify({ market_id: mercadoId, option: opcion, amount: monto }),
    });
  } catch {
    return { ok: false, status: 0, mensaje: "No se pudo conectar con el motor. Intenta de nuevo." };
  }

  let cuerpo = {};
  try {
    cuerpo = await resp.json();
  } catch {
    /* el motor respondio sin JSON */
  }

  if (resp.ok) return { ok: true, apuesta: cuerpo };
  if (resp.status === 401) {
    return { ok: false, status: 401, mensaje: "Tu sesion vencio. Inicia sesion de nuevo." };
  }
  return { ok: false, status: resp.status, mensaje: cuerpo.error ?? "No se pudo registrar la apuesta" };
}
