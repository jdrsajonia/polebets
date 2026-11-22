// Laboratorio 6 / Escenario 5 (rate limiting) — prueba de carga del login de PoleBet.
//
// Rama "antes": p2/sin-seguridad  -> Gateway en http://localhost  (puerto 80)
// Rama "después": con TLS         -> Gateway en https://localhost (puerto 443)
//
// Uso (PowerShell):
//   $env:BASE_URL="http://localhost"; $env:EMAIL="jugador@correo.com"; $env:PASSWORD="su-clave"
//   k6 run performance_test.js
//
// Para repetir con HTTPS y el certificado autofirmado del Gateway:
//   $env:BASE_URL="https://localhost"; k6 run --insecure-skip-tls-verify performance_test.js

import http from 'k6/http';
import { sleep, check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@correo.com';
const PASSWORD = __ENV.PASSWORD || 'cambiar-clave';

// Cuenta aparte las respuestas 429 (rate limiting), para no mezclarlas con errores reales.
const limitadas = new Counter('respuestas_429');

// Los 429 no cuentan como fallo: así http_req_failed solo muestra errores reales (5xx, timeouts).
// 401 sí cuenta como fallo, porque aquí la contraseña es correcta.
http.setResponseCallback(http.expectedStatuses(200, 429));

export const options = {
  stages: [
    { duration: '30s', target: 1 },
    { duration: '30s', target: 50 },
    { duration: '30s', target: 200 },
    { duration: '30s', target: 500 },
    { duration: '30s', target: 2000 },
    { duration: '30s', target: 0 },
  ],
  insecureSkipTLSVerify: true,
};

export default function () {
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } },
  );

  if (res.status === 429) {
    limitadas.add(1);
  }

  check(res, {
    'status es 200 o 429': (r) => r.status === 200 || r.status === 429,
  });

  sleep(1);
}
