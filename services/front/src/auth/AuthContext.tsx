// Sesion del usuario para toda la app (HU-02).
//
// Como funciona:
//   1. Al montar, se le pregunta a Pagos quien es el dueño de la cookie
//      (GET /auth/me). Mientras tanto `cargando` es true y RutaProtegida no
//      decide nada, asi que recargar la pagina no saca al usuario.
//   2. `iniciarSesion(email, password)` llama a POST /auth/login. Pagos deja
//      la cookie HttpOnly con el JWT y devuelve el usuario.
//   3. `cerrarSesion()` llama a POST /auth/logout y limpia el usuario.
//   4. Si cualquier peticion responde 401 (token vencido), http.ts avisa y la
//      sesion se limpia sola; RutaProtegida lleva entonces a /login.
//
// El resto de la app solo usa useAuth(): no sabe si el login es propio u OAuth.

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { configurarHttp } from "../api/http";
import * as apiAuth from "./api";

// Roles como los define el modelo de datos de Pagos y el contrato del JWT.
export type Rol = "player" | "admin";

// Respuesta de Pagos en /auth/login y /auth/me.
export interface Usuario {
  id: string; // UUID (claim "sub" del JWT)
  nombre: string;
  email: string;
  rol: Rol;
}

export interface ValorAuth {
  usuario: Usuario | null;
  cargando: boolean; // true mientras se averigua si hay sesion
  iniciarSesion: (email: string, password: string) => Promise<Usuario>; // lanza ErrorHttp si falla
  cerrarSesion: () => Promise<void>; // nunca falla: si Pagos no responde, igual limpia
  avisoAcceso: string | null; // mensaje de "acceso denegado" (403)
  descartarAviso: () => void;
}

const AuthContexto = createContext<ValorAuth | null>(null);

interface Props {
  children: ReactNode;
  // Fija la sesion sin llamar a Pagos (pruebas). null = sin sesion.
  // undefined = averiguarla con GET /auth/me (comportamiento normal).
  usuarioInicial?: Usuario | null;
}

export function AuthProvider({ children, usuarioInicial }: Props) {
  // Si la sesion viene dada (en pruebas) no hay nada que cargar. En
  // produccion usuarioInicial es undefined y se consulta /auth/me.
  const [sesionFija] = useState<Usuario | null | undefined>(() => usuarioInicial);
  const [usuario, setUsuario] = useState<Usuario | null>(sesionFija ?? null);
  const [cargando, setCargando] = useState(sesionFija === undefined);
  const [avisoAcceso, setAvisoAcceso] = useState<string | null>(null);

  // Limpia la sesion solo en el front (no llama a Pagos). Es lo que se hace
  // ante un 401: el token ya no sirve, no hay nada que pedirle al servidor.
  const limpiarSesion = useCallback(() => setUsuario(null), []);

  // Conecta http.ts con la sesion: un 401 la limpia y un 403 muestra el aviso.
  useEffect(() => {
    configurarHttp({ sesionVencida: limpiarSesion, accesoDenegado: setAvisoAcceso });
    return () => configurarHttp({});
  }, [limpiarSesion]);

  // 1. Al montar: ¿ya hay una sesion abierta? (p. ej. tras recargar la pagina)
  useEffect(() => {
    if (sesionFija !== undefined) return;
    // AbortController evita actualizar el estado si el componente se desmonta
    // antes de que llegue la respuesta (y el doble montaje de StrictMode).
    const control = new AbortController();
    apiAuth
      .obtenerSesion(control.signal)
      .then((actual) => setUsuario(actual))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return;
        // Pagos caido o sin red: se arranca sin sesion y el usuario vera el
        // error real cuando intente iniciar sesion.
        setUsuario(null);
      })
      .finally(() => {
        if (!control.signal.aborted) setCargando(false);
      });
    return () => control.abort();
  }, [sesionFija]);

  // 2. Login: si falla, el ErrorHttp sube al formulario para mostrar el mensaje.
  const iniciarSesion = useCallback(async (email: string, password: string) => {
    const nuevo = await apiAuth.login(email, password);
    setAvisoAcceso(null);
    setUsuario(nuevo);
    return nuevo;
  }, []);

  // 3. Logout: se limpia el front aunque Pagos no responda; quedarse "medio
  // logueado" seria peor. La cookie vence sola de todas formas.
  const cerrarSesion = useCallback(async () => {
    try {
      await apiAuth.logout();
    } catch {
      /* se ignora a proposito */
    } finally {
      setAvisoAcceso(null);
      setUsuario(null);
    }
  }, []);

  const descartarAviso = useCallback(() => setAvisoAcceso(null), []);

  const valor = useMemo<ValorAuth>(
    () => ({ usuario, cargando, iniciarSesion, cerrarSesion, avisoAcceso, descartarAviso }),
    [usuario, cargando, iniciarSesion, cerrarSesion, avisoAcceso, descartarAviso],
  );

  return <AuthContexto.Provider value={valor}>{children}</AuthContexto.Provider>;
}

// Hook para leer la sesion.
export function useAuth(): ValorAuth {
  const valor = useContext(AuthContexto);
  if (!valor) throw new Error("useAuth debe usarse dentro de <AuthProvider>");
  return valor;
}
