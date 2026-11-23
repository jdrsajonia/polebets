// Escenario 5 — una corrida por nivel de usuarios contra /api/eventos (Orquestador),
// para armar la curva de rendimiento / rodilla de ese endpoint.
// Uso (Git Bash):
//   export EMAIL="jugador@prueba.com"; export PASSWORD="12345678"
//   VUS=50 k6 run --summary-export=resultados/api-antes-50.json scripts/api_niveles.js

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';
const PASSWORD = __ENV.PASSWORD || '12345678';
const VUS = Number(__ENV.VUS || 1);

http.setResponseCallback(http.expectedStatuses(200, 429));

export const options = {
  vus: VUS,
  duration: '30s',
  insecureSkipTLSVerify: true,
};

export function setup() {
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' }, timeout: '10s' },
  );
  if (res.status !== 200) {
    throw new Error(`No se pudo loguear: ${res.status} ${res.body}`);
  }
  const token = res.cookies.session[0].value;
  return { cookie: `session=${token}` };
}

export default function (data) {
  const res = http.get(`${BASE_URL}/api/eventos`, {
    headers: { Cookie: data.cookie },
    timeout: '10s',
  });
  check(res, { 'status 200 o 429': (r) => [200, 429].includes(r.status) });
}
