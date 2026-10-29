// Usuarios de mentira para las pruebas, uno por rol. Se pasan a
// <AuthProvider usuarioInicial={...}> para fijar la sesion sin llamar a Pagos.
// No forma parte del codigo de produccion: solo lo importan archivos *.test.
import type { Rol, Usuario } from "./AuthContext";

export const USUARIOS_SIMULADOS: Record<Rol, Usuario> = {
  player: { id: "00000000-0000-4000-8000-000000000001", nombre: "Jugador Demo", email: "jugador@demo.local", rol: "player" },
  admin: { id: "00000000-0000-4000-8000-000000000002", nombre: "Admin Demo", email: "admin@demo.local", rol: "admin" },
};
