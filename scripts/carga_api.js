// Escenario 5 - ráfaga contra el resto de la API (no el login).
// Login una vez en setup(), luego ataca GET /api/eventos con la cookie real.
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';
const PASSWORD = __ENV.PASSWORD || '12345678';

http.setResponseCallback(http.expectedStatuses(200, 429));

export const options = {
  vus: Number(__ENV.VUS || 50),
  duration: '30s',
  insecureSkipTLSVerify: true,
};

// setup() corre una sola vez, antes de los usuarios virtuales.
export function setup() {
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' }, timeout: '10s' },
  );
  if (res.status !== 200) {
    throw new Error(`No se pudo loguear: ${res.status} ${res.body}`);
  }
  // Cookie real del login; se pasa a mano en cada petición de los VUs.
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
