# Front (HU2-03): servidor Node con la SPA y la pagina del usuario con SSR.
# Vista de despliegue: contenedor "front", Node.js, puerto 3000 (solo interno;
# los jugadores entran por el Gateway).

# ---- build: SPA + entrada de hidratacion (dist/) y bundle SSR (dist-ssr/) ----
FROM node:22-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install

COPY . .
RUN npm run build \
    # En la imagen final solo hacen falta react y react-dom (los usa el SSR).
    && npm prune --omit=dev

# ---- runtime ----
FROM node:22-alpine

WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000

COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY --from=build /app/dist-ssr ./dist-ssr
COPY server ./server

# Sin root: el puerto 3000 no necesita privilegios.
USER node
EXPOSE 3000

CMD ["node", "server/servidor.mjs"]
