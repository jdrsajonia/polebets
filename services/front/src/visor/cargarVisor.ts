// Carga el visor de pista (HU2-15): el runtime de Blazor WebAssembly que
// registra el elemento <visor-pista>. Sus archivos (_framework/ y _content/)
// los copia el Dockerfile del Front a la raiz de dist/ al construir la imagen.
//
// Se carga una sola vez para toda la app y solo cuando se abre la carrera: los
// ~2 MB del runtime no se descargan en las demas paginas.

const URL_BLAZOR = "/_framework/blazor.webassembly.js";
export const ETIQUETA_VISOR = "visor-pista";
// Si el runtime arranca pero el elemento no aparece en este tiempo, se da por fallido.
const ESPERA_ELEMENTO_MS = 15000;

interface Blazor {
  start(opciones?: Record<string, unknown>): Promise<void>;
}

declare global {
  interface Window {
    Blazor?: Blazor;
  }
}

let carga: Promise<void> | null = null;

// Agrega el script de Blazor (sin arrancar solo) y lo arranca. Los archivos
// se piden siempre desde la raiz, aunque la pagina este en una ruta anidada.
function arrancar(): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = URL_BLAZOR;
    script.setAttribute("autostart", "false");
    script.onerror = () => reject(new Error("No se pudo descargar el visor"));
    script.onload = () => {
      const blazor = window.Blazor;
      if (!blazor) {
        reject(new Error("El visor no está disponible en este servidor"));
        return;
      }
      blazor
        .start({
          loadBootResource: (_tipo: string, nombre: string) => `/_framework/${nombre}`,
        })
        .then(() => esperarElemento())
        .then(resolve, reject);
    };
    document.head.appendChild(script);
  });
}

// Espera a que Blazor defina <visor-pista> (lo hace al terminar de arrancar).
function esperarElemento(): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const limite = setTimeout(() => reject(new Error("El visor no terminó de arrancar")), ESPERA_ELEMENTO_MS);
    customElements.whenDefined(ETIQUETA_VISOR).then(() => {
      clearTimeout(limite);
      resolve();
    });
  });
}

// Devuelve una promesa que se cumple cuando <visor-pista> ya se puede usar.
// Si falla, la siguiente llamada lo vuelve a intentar.
export function cargarVisor(): Promise<void> {
  if (customElements.get(ETIQUETA_VISOR)) return Promise.resolve();
  carga ??= arrancar().catch((error: unknown) => {
    carga = null;
    throw error;
  });
  return carga;
}
