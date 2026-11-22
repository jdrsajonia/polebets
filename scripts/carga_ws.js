// Escenario 5 - agotamiento de conexiones WebSocket (/api/ws).
// Login una vez, luego cada VU abre una conexión WS y la mantiene abierta.
import ws from 'k6/ws';
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';
const PASSWORD = __ENV.PASSWORD || '12345678';
const HOLD_MS = Number(__ENV.HOLD_MS || 20000);

http.setResponseCallback(http.expectedStatuses(200, 429));

const rechazadas = new Counter('ws_rechazadas');

export const options = {
  vus: Number(__ENV.VUS || 50),
  iterations: Number(__ENV.VUS || 50),
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
