// Pantalla /registro (HU-01): crear una cuenta de jugador.
//
// Criterios de aceptacion que cubre:
//   - Datos validos: Pagos crea el jugador (rol "player", saldo 0) y se inicia
//     sesion de una vez con el mismo correo y contraseña, para no obligar a
//     escribirlos otra vez en /login. Se entra a /carrera.
//   - Correo ya registrado: se muestra el mensaje de Pagos (409).
//   - Datos invalidos: se avisa por campo antes de llamar a Pagos, con las
//     mismas reglas del backend (nombre 1-100, correo valido, contraseña 8-128).
//   - Si ya hay sesion, /registro no tiene sentido y se va al inicio.
//
// Reutiliza los estilos de /login para que ambas pantallas se vean iguales.

import { useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { ErrorHttp } from "../api/http";
import { useAuth } from "../auth/AuthContext";
import { registrar } from "../auth/api";
import { destinoTrasLogin } from "../auth/destino";
import estilos from "./Login.module.css";

// Mismos limites que RegisterRequest en Pagos (app/schemas/auth.py).
export const MAX_NOMBRE = 100;
export const MIN_PASSWORD = 8;
export const MAX_PASSWORD = 128;

// Validacion basica de forma; la definitiva la hace Pagos (EmailStr).
const PATRON_EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface ErroresCampos {
  nombre?: string;
  email?: string;
  password?: string;
  confirmacion?: string;
}

export function validarRegistro(nombre: string, email: string, password: string, confirmacion: string): ErroresCampos {
  const errores: ErroresCampos = {};
  const n = nombre.trim();
  const e = email.trim();

  if (!n) errores.nombre = "Escribe tu nombre.";
  else if (n.length > MAX_NOMBRE) errores.nombre = `El nombre puede tener máximo ${MAX_NOMBRE} caracteres.`;

  if (!e) errores.email = "Escribe tu correo.";
  else if (!PATRON_EMAIL.test(e)) errores.email = "Escribe un correo válido.";

  if (!password) errores.password = "Escribe una contraseña.";
  else if (password.length < MIN_PASSWORD) errores.password = `La contraseña debe tener al menos ${MIN_PASSWORD} caracteres.`;
  else if (password.length > MAX_PASSWORD) errores.password = `La contraseña puede tener máximo ${MAX_PASSWORD} caracteres.`;

  if (!confirmacion) errores.confirmacion = "Repite la contraseña.";
  else if (confirmacion !== password) errores.confirmacion = "Las contraseñas no coinciden.";

  return errores;
}

const hayErrores = (e: ErroresCampos) => Object.values(e).some(Boolean);

export default function Registro() {
  const { usuario, cargando, iniciarSesion } = useAuth();
  const navegar = useNavigate();

  const [nombre, setNombre] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmacion, setConfirmacion] = useState("");
  const [errores, setErrores] = useState<ErroresCampos>({});
  const [errorGeneral, setErrorGeneral] = useState<string | null>(null);
  const [enviando, setEnviando] = useState(false);

  if (cargando) return <p role="status">Cargando…</p>;
  // Ya hay sesion: al inicio de su rol. Durante el envio no, lo hace onSubmit.
  if (usuario && !enviando) return <Navigate to={destinoTrasLogin(usuario.rol)} replace />;

  async function onSubmit(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (enviando) return; // evita doble envio con doble clic

    const nuevos = validarRegistro(nombre, email, password, confirmacion);
    setErrores(nuevos);
    setErrorGeneral(null);
    if (hayErrores(nuevos)) return;

    setEnviando(true);
    const correo = email.trim();

    // 1. Crear la cuenta.
    try {
      await registrar(nombre.trim(), correo, password);
    } catch (error) {
      setErrorGeneral(error instanceof ErrorHttp ? error.message : "Ocurrió un error inesperado.");
      setEnviando(false);
      return;
    }

    // 2. Entrar con la cuenta recien creada (Pagos deja la cookie).
    try {
      const nuevo = await iniciarSesion(correo, password);
      navegar(destinoTrasLogin(nuevo.rol), { replace: true });
    } catch {
      // La cuenta si quedo creada; solo fallo el login automatico.
      navegar("/login", { replace: true });
    }
  }

  // Props comunes de cada campo: estado de error accesible y bloqueo al enviar.
  function propsCampo(campo: keyof ErroresCampos) {
    const id = `registro-${campo}`;
    return {
      id,
      "aria-invalid": errores[campo] ? true : undefined,
      "aria-describedby": errores[campo] ? `${id}-error` : undefined,
      disabled: enviando,
    };
  }

  function mensaje(campo: keyof ErroresCampos) {
    return (
      errores[campo] && (
        <span id={`registro-${campo}-error`} className={estilos.errorCampo}>
          {errores[campo]}
        </span>
      )
    );
  }

  return (
    <section className={`tarjeta ${estilos.contenedor}`}>
      <h1 className="titulo-pagina">Crear cuenta</h1>
      <p className="texto-secundario">Regístrate como jugador para apostar en las carreras en vivo.</p>

      {errorGeneral && (
        <p className={estilos.error} role="alert">
          {errorGeneral}
        </p>
      )}

      {/* noValidate: los mensajes los pone la app, en español y por campo. */}
      <form className={estilos.formulario} onSubmit={onSubmit} noValidate>
        <div className={estilos.campo}>
          <label htmlFor="registro-nombre">Nombre</label>
          <input
            {...propsCampo("nombre")}
            type="text"
            autoComplete="name"
            maxLength={MAX_NOMBRE}
            value={nombre}
            onChange={(e) => setNombre(e.target.value)}
          />
          {mensaje("nombre")}
        </div>

        <div className={estilos.campo}>
          <label htmlFor="registro-email">Correo</label>
          <input
            {...propsCampo("email")}
            type="email"
            autoComplete="email"
            inputMode="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          {mensaje("email")}
        </div>

        <div className={estilos.campo}>
          <label htmlFor="registro-password">Contraseña</label>
          <input
            {...propsCampo("password")}
            type="password"
            autoComplete="new-password"
            maxLength={MAX_PASSWORD}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {mensaje("password")}
        </div>

        <div className={estilos.campo}>
          <label htmlFor="registro-confirmacion">Repite la contraseña</label>
          <input
            {...propsCampo("confirmacion")}
            type="password"
            autoComplete="new-password"
            maxLength={MAX_PASSWORD}
            value={confirmacion}
            onChange={(e) => setConfirmacion(e.target.value)}
          />
          {mensaje("confirmacion")}
        </div>

        <button type="submit" className="boton" disabled={enviando}>
          {enviando ? "Creando cuenta…" : "Crear cuenta"}
        </button>
      </form>

      <p className="texto-secundario">
        ¿Ya tienes cuenta? <Link to="/login">Inicia sesión</Link>
      </p>
    </section>
  );
}
