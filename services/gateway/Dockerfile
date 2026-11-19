# API Gateway — STUB base (T2-04). Nginx como única entrada de los jugadores.
FROM nginx:1.27-alpine

# conf.d/ se incluye dentro del bloque http; polebet/ lo incluye nginx.conf dentro del server.
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY limites-zonas.conf /etc/nginx/conf.d/limites-zonas.conf
COPY rutas.conf admin.conf /etc/nginx/polebet/

EXPOSE 80
