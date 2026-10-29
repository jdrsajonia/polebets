# API Gateway (stub base — T2-04 / lo completa T2-01)

Esqueleto del **API Gateway** (Nginx). Dueño: **Tomás** (T2-01).

Hoy solo responde `GET /health` para que el contenedor arranque. El archivo
`nginx.conf` ya trae **esbozadas y comentadas** las rutas reales: son el punto
de partida de T2-01, no hay que empezar de cero.

## Qué implementar aquí (T2-01)

- Única entrada web de los jugadores; es uno de los dos únicos puertos publicados (80).
- `auth_request` contra Usuarios en cada ruta protegida; sin sesión válida → 401.
- **Borrar** los `X-User-Id` / `X-User-Rol` que mande el cliente y poner solo los que
  devuelve `auth_request` (requisito explícito del contrato del Orquestador).
- Enrutar: `login`/`logout` → Usuarios; resto del REST → Orquestador; WebSocket → Motor (Difusor).
- WebSocket que no se corte a los 60 s (`proxy_read_timeout` alto).

## Redes

Vive en la red **borde** (de cara al host) y en la **interna** (para alcanzar
Usuarios, Orquestador y Motor). No toca ninguna base de datos.
