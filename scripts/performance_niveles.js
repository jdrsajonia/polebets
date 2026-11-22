// Laboratorio 6 — una corrida por nivel de usuarios, para armar la curva de rendimiento.
// Uso (Git Bash):
//   export EMAIL="jugador@prueba.com"; export PASSWORD="clave"
//   for v in 1 50 200 500 2000; do VUS=$v k6 run --summary-export=resultados/antes-$v.json scripts/performance_niveles.js; done

import http from 'k6/http';
import { sleep, check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';
const PASSWORD = __ENV.PASSWORD || 'cambiar-clave';
const VUS = Number(__ENV.VUS || 1);

// 429 (rate limiting) no cuenta como fallo; sí se cuenta aparte.
http.setResponseCallback(http.expectedStatuses(200, 429));
const limitadas = new Counter('respuestas_429');

export const options = {
  vus: VUS,
  duration: '30s',
  insecureSkipTLSVerify: true,
};

export default function () {
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' }, timeout: '10s' },
  );
  if (res.status === 429) {
    limitadas.add(1);
  }
  check(res, { 'status es 200 o 429': (r) => r.status === 200 || r.status === 429 });
  sleep(1);
}
