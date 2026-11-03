# Visor de pista (C# / Blazor WebAssembly) — HU2-15

El elemento `<visor-pista>` dibuja la carrera vista desde arriba con los autos en
movimiento, dentro de `/carrera` (RF-20). Es un componente de presentación propio
(RNF-02): C# compilado a WebAssembly que corre en el navegador del jugador.

```
Motor (Difusor) ──WebSocket /api/ws──► <visor-pista>   pista · coordenadas · posiciones
                  (Gateway, cookie)        │
React (/carrera) ── atributos ────────────┘   seguir · mostrar-codigos
```

- Abre **su propio** WebSocket a `/api/ws` (Vista C&C: «SPA y Visor → Gateway → Motor»).
  El navegador manda la cookie de sesión sola y el Gateway la valida como cualquier
  conexión.
- Escucha tres mensajes del Motor (contrato en `services/motor/README.md`):
  - `pista`: el trazado de una vuelta completa.
  - `coordenadas`: x/y de los autos, cada ~1 s.
  - `posiciones`: código, color y puesto de cada piloto (HU2-14).
- React solo le dice a quién seguir. No le pasa datos.

## Atributos

| Atributo | Valor | Para qué |
| --- | --- | --- |
| `seguir` | número del carro (`"0"` = ninguno) | Lo resalta: más grande, con anillo y siempre con su código |
| `mostrar-codigos` | `"true"` / `"false"` | Código de tres letras junto a cada auto |
| `mostrar-fps` | `"true"` / `"false"` | Cuadros por segundo en la esquina (en el Front: `VITE_SHOW_LATENCY`) |
| `demo` | `"true"` | Carrera de mentira sin Motor, para desarrollar |
| `url-ws` | `ws://…` | Otro WebSocket (por defecto `ws(s)://<host>/api/ws`) |

El elemento también expone `data-fps` y `data-autos` en su `<div>` raíz (para medir
RNF-13 desde una prueba).

## Cómo cumple HU2-15

- **Dibuja la pista a partir de una vuelta completa.** El Motor toma la vuelta más rápida
  sin salida de pits y manda las coordenadas de ese piloto en esa vuelta (unos 360
  puntos). El visor las ajusta a un lienzo SVG sin deformarlas e invierte la y (en
  OpenF1 crece hacia arriba).
- **30 cuadros por segundo o más, suavizando entre muestras.** OpenF1 trae ~3,7 muestras
  por segundo y por auto, y el Motor las manda en tramos de 1 s. El visor dibuja cada
  ~16 ms con 1,6 s de retraso, así siempre tiene una muestra antes y otra después, e
  interpola en línea recta. El reloj de carrera se estima con una media móvil para que
  la red no produzca saltitos. Medido en Chromium con la carrera real: **62–63 fps con
  19 autos**.
- **Elegir un piloto en la tabla lo resalta.** La tabla de posiciones de `/carrera` cambia
  el atributo `seguir` y Blazor lo observa. El seguido se dibuja encima de los demás.

## Estructura

```
Visor/                  app Blazor WebAssembly
  Program.cs            registra el componente como <visor-pista>
  VisorPista.razor      dibujo SVG, bucle de cuadros e indicador de estado
  ConexionMotor.cs      WebSocket con reconexión (1, 2, 4, 8 y cada 10 s)
  TextoSvg.razor        <text> de SVG (en un bloque @if Razor lo confundiría con el suyo)
  wwwroot/index.html    página de desarrollo con el visor en modo demo (no se despliega)
Visor.Nucleo/           lógica sin Blazor ni red, probada con xUnit
  Mensajes.cs           pista, coordenadas y posiciones (System.Text.Json generado)
  Proyeccion.cs         coordenadas de OpenF1 → lienzo SVG
  RelojCarrera.cs       qué instante de la carrera se dibuja
  Interpolador.cs       posición de cada auto entre muestras
  EstadoVisor.cs        junta todo y arma cada cuadro
  CarreraDemo.cs        carrera de mentira (atributo demo)
Visor.Pruebas/          pruebas xUnit del núcleo
```

## Despliegue

No tiene contenedor propio. El Dockerfile del Front lo publica con el SDK de .NET 10
(contexto adicional `visor` del compose) y copia `_framework/` y `_content/` a la raíz de
su `dist/`. El servidor Node los sirve con `application/wasm` y, si el navegador acepta
Brotli, manda la versión `.br` que ya genera `dotnet publish`: ~6 MB que bajan a ~2 MB.

`src/visor/cargarVisor.ts` del Front carga el runtime una sola vez y solo al abrir
`/carrera`. Con `npm run dev` (sin Docker) no hay `_framework/` y la tarjeta muestra
«Visor no disponible».

## Desarrollar y probar

Sin .NET instalado, con Docker desde `services/visor`:

```bash
# pruebas del núcleo
docker run --rm -v "$PWD":/src -w /src mcr.microsoft.com/dotnet/sdk:10.0 dotnet test Visor.Pruebas

# publicar y ver la demo en http://localhost:8765 (sin Motor ni sesión)
docker run --rm -v "$PWD":/src -v /tmp/visor:/out -w /src mcr.microsoft.com/dotnet/sdk:10.0 \
  dotnet publish Visor -c Release -o /out
cd /tmp/visor/wwwroot && python3 -m http.server 8765
```

Con .NET 10 instalado basta `dotnet test Visor.Pruebas` y el mismo `dotnet publish`.
