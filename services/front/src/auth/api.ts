// Llamadas REST de la sesion y el registro (HU2-01 y HU2-02, prototipo 2).
//
// Todo entra por el API Gateway (mismo origen, prefijo /api):
//
//   POST /api/auth/login  { email, password } -> Usuario + cookie HttpOnly "session"   (Usuarios)
//   POST /api/auth/logout -> 204, borra la cookie                                      (Usuarios)
//   GET  /api/auth/me     -> Usuario, o 401 si no hay sesion                           (Usuarios)
//   POST /api/registro    { nombre, email, contrasena } -> 201 { usuario }, sin cookie  (Orquestador)
//
// El JWT nunca pasa por aqui: lo guarda el navegador en una cookie HttpOnly
// que JavaScript no puede leer. http.ts la envia sola (credentials: "include")
// y el Gateway la valida en cada ruta protegida.

import { ErrorHttp, http } from "../api/http";
import type { Usuario } from "./AuthContext";

// Mensaje de HU2-02: no dice si fallo el correo o la contraseña.
export const MENSAJE_CREDENCIALES = "Correo o contraseña incorrectos";

// Pregunta a Usuarios quien es el dueño de la cookie. Devuelve null si no hay
// sesion o ya vencio (401); cualquier otro error se propaga.
export async function obtenerSesion(signal?: AbortSignal): Promise<Usuario | null> {
  try {
    return await http.get<Usuario>("api", "/auth/me", { signal });
  } catch (error) {
    if (error instanceof ErrorHttp && error.status === 401) return null;
    throw error;
  }
}

// Envia las credenciales. Si son correctas Usuarios responde el usuario y deja
// la cookie puesta.
export async function login(email: string, password: string): Promise<Usuario> {
  try {
    return await http.post<Usuario>("api", "/auth/login", { email, password });
  } catch (error) {
    // http.ts traduce todo 401 como "Tu sesión venció", que aqui no aplica:
    // en el login un 401 significa credenciales invalidas.
    if (error instanceof ErrorHttp && error.status === 401) {
      throw new ErrorHttp(401, MENSAJE_CREDENCIALES);
    }
    throw error;
  }
}

// Le pide a Usuarios que borre la cookie.
export async function logout(): Promise<void> {
  await http.post<void>("api", "/auth/logout");
}

// ---------------------------------------------------------------------------
// HU2-01: registro orquestado
// ---------------------------------------------------------------------------

// Mensaje cuando el registro rechaza los datos (400 cuerpo_invalido): el
// Orquestador no dice que campo fallo. El formulario ya valida lo mismo antes
// de enviar, asi que esto solo pasaria si cambian las reglas del backend.
export const MENSAJE_DATOS_INVALIDOS = "Revisa los datos: nombre, correo válido y contraseña de al menos 8 caracteres.";

// Crea un jugador (rol "player") y su billetera en 0. NO deja cookie: para
// entrar hay que llamar a login despues. Un 409 llega con el mensaje del
// Orquestador («Este correo ya está registrado»).
export async function registrar(nombre: string, email: string, password: string): Promise<Usuario> {
  try {
    const { usuario } = await http.post<{ usuario: Usuario }>("api", "/registro", { nombre, email, contrasena: password });
    return usuario;
  } catch (error) {
    if (error instanceof ErrorHttp && (error.status === 400 || error.status === 422)) {
      throw new ErrorHttp(error.status, MENSAJE_DATOS_INVALIDOS);
    }
    throw error;
  }
}
