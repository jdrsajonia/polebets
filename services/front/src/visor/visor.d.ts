// Tipos del elemento <visor-pista> (Blazor, services/visor) para usarlo en JSX.
// React 18 pasa las props de un elemento personalizado como atributos de texto.

import "react";

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "visor-pista": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & {
        seguir?: string; // numero del piloto a resaltar ("0" = ninguno)
        "mostrar-codigos"?: "true" | "false";
        "mostrar-fps"?: "true" | "false";
      };
    }
  }
}
