// Llamadas REST de la sesion contra Pagos (HU-01 y HU-02).
//
//   POST /auth/register { name, email, password } -> Usuario (201), sin cookie
//   POST /auth/login   { email, password } -> Usuario  + cookie HttpOnly "session"
//   POST /auth/logout  -> 204, borra la cookie
//   GET  /auth/me      -> Usuario, o 401 si no hay sesion
//
// El JWT nunca pasa por aqui: lo guarda el navegador en una cookie HttpOnly
// que JavaScript no puede leer. http.ts la envia sola en cada peticion
// (credentials: "include"), tanto a Pagos como al motor.

import { ErrorHttp, http } from "../api/http";
import type { Usuario } from "./AuthContext";

// Mensaje de HU-02: no dice si fallo el correo o la contraseña.
export const MENSAJE_CREDENCIALES = "Correo o contraseña incorrectos";

// Pregunta a Pagos quien es el dueño de la cookie. Devuelve null si no hay
// sesion o ya vencio (401); cualquier otro error se propaga.
export async function obtenerSesion(signal?: AbortSignal): Promise<Usuario | null> {
  try {
    return await http.get<Usuario>("pagos", "/auth/me", { signal });
  } catch (error) {
    if (error instanceof ErrorHttp && error.status === 401) return null;
    throw error;
  }
}

// Envia las credenciales. Si son correctas Pagos responde el usuario y deja
// la cookie puesta.
export async function login(email: string, password: string): Promise<Usuario> {
  try {
    return await http.post<Usuario>("pagos", "/auth/login", { email, password });
  } catch (error) {
    // http.ts traduce todo 401 como "Tu sesión venció", que aqui no aplica:
    // en el login un 401 significa credenciales invalidas.
    if (error instanceof ErrorHttp && error.status === 401) {
      throw new ErrorHttp(401, MENSAJE_CREDENCIALES);
    }
    throw error;
  }
}

// Le pide a Pagos que borre la cookie.
export async function logout(): Promise<void> {
  await http.post<void>("pagos", "/auth/logout");
}

// ---------------------------------------------------------------------------
// HU-01: registro
// ---------------------------------------------------------------------------

// Mensaje cuando Pagos rechaza los datos (422). FastAPI responde una lista de
// errores por campo que no sirve para mostrar tal cual; el formulario ya
// valida lo mismo antes de enviar, asi que esto solo pasaria si cambian las
// reglas del backend.
export const MENSAJE_DATOS_INVALIDOS = "Revisa los datos: nombre, correo válido y contraseña de al menos 8 caracteres.";

// Crea un jugador (rol "player", saldo 0). Pagos NO deja cookie: para entrar
// hay que llamar a login despues. Un 409 llega con el mensaje de Pagos
// («Este correo ya está registrado»).
export async function registrar(nombre: string, email: string, password: string): Promise<Usuario> {
  try {
    return await http.post<Usuario>("pagos", "/auth/register", { name: nombre, email, password });
  } catch (error) {
    if (error instanceof ErrorHttp && error.status === 422) {
      throw new ErrorHttp(422, MENSAJE_DATOS_INVALIDOS);
    }
    throw error;
  }
}
