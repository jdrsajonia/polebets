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

# ---- visor de pista (HU2-15): Blazor WebAssembly en C# ----
# El codigo viene de services/visor por el contexto adicional "visor" del
# docker-compose.yml. Publica _framework/ (runtime y .wasm) y _content/, que
# se sirven desde la raiz del front para que <visor-pista> funcione en /carrera.
FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build-visor

WORKDIR /visor
ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 \
    DOTNET_NOLOGO=1

# Primero los .csproj, para que Docker guarde en cache los paquetes de NuGet.
COPY --from=visor Visor.Nucleo/Visor.Nucleo.csproj Visor.Nucleo/
COPY --from=visor Visor/Visor.csproj Visor/
RUN dotnet restore Visor/Visor.csproj

# Solo el codigo: el .dockerignore no aplica a un contexto adicional y un
# obj/ local romperia la restauracion de arriba.
COPY --from=visor Visor.Nucleo/*.cs Visor.Nucleo/
COPY --from=visor Visor/*.cs Visor/*.razor Visor/
COPY --from=visor Visor/wwwroot Visor/wwwroot
RUN dotnet publish Visor/Visor.csproj -c Release --no-restore -o /out

# ---- runtime ----
FROM node:22-alpine

WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000

COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
# El visor va junto a la SPA (sin su index.html de desarrollo).
COPY --from=build-visor /out/wwwroot/_framework ./dist/_framework
COPY --from=build-visor /out/wwwroot/_content ./dist/_content
COPY --from=build /app/dist-ssr ./dist-ssr
COPY server ./server

# Sin root: el puerto 3000 no necesita privilegios.
USER node
EXPOSE 3000

CMD ["node", "server/servidor.mjs"]
