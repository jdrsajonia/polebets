// Escenario 5 - fuerza bruta contra el login: prueba cientos de contraseñas
// distintas seguidas, sin que nada las frene (en "antes") o con el límite
// del Gateway puesto (en "después").
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost';
const EMAIL = __ENV.EMAIL || 'jugador@prueba.com';

http.setResponseCallback(http.expectedStatuses(200, 401, 429));

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: '30s',
  insecureSkipTLSVerify: true,
};

export default function () {
  const intento = `clave-${Math.floor(Math.random() * 100000)}`;
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ email: EMAIL, password: intento }),
    { headers: { 'Content-Type': 'application/json' }, timeout: '10s' },
  );
  check(res, { 'status 200, 401 o 429': (r) => [200, 401, 429].includes(r.status) });
}
