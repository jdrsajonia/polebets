// Escenario 5 — una corrida por nivel de usuarios contra /api/ws (Motor),
// para armar la curva de rendimiento / rodilla de WebSocket.
// Cada VU abre UNA conexión y la mantiene abierta HOLD_MS; se cuenta cuántas
// conexiones no logran abrirse (rechazadas) a medida que sube el nivel.
// Uso (Git Bash):
//   export EMAIL="jugador@prueba.com"; export PASSWORD="12345678"
//   VUS=50 k6 run --summary-export=resultados/ws-antes-50.json scripts/ws_niveles.js

import ws from 'k6/ws';
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';
const PASSWORD = __ENV.PASSWORD || '12345678';
const HOLD_MS = Number(__ENV.HOLD_MS || 5000);
const VUS = Number(__ENV.VUS || 1);

http.setResponseCallback(http.expectedStatuses(200, 429));

const rechazadas = new Counter('ws_rechazadas');

export const options = {
  vus: VUS,
  iterations: VUS,
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
  const wsUrl = BASE_URL.replace(/^http/, 'ws') + '/api/ws';
  const res = ws.connect(wsUrl, { headers: { Cookie: data.cookie } }, function (socket) {
    socket.on('open', function () {
      socket.setTimeout(function () {
        socket.close();
      }, HOLD_MS);
    });
    socket.on('error', function () {
      rechazadas.add(1);
    });
  });
  check(res, { 'conexión abierta (101)': (r) => r && r.status === 101 });
  if (!res || res.status !== 101) {
    rechazadas.add(1);
  }
}
