// Pantalla /registro (HU-01): formulario de nombre, correo y contraseña.
//
// Criterios de aceptacion que cubre:
//   - Correo nuevo + datos validos: Pagos crea la cuenta con rol player y
//     saldo 0, y se va a /login con un aviso de exito.
//   - Correo repetido: «Este correo ya está registrado» (viene del 409).
//   - Campo vacio o correo mal formado: se avisa antes de llamar a Pagos.
//   - La contrasena nunca se guarda en el front: solo viaja a Pagos.

import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ErrorHttp } from "../api/http";
import { register } from "../auth/api";
import estilos from "./Registro.module.css";

// Errores de validacion por campo, antes de llamar a Pagos.
interface ErroresCampos {
  nombre?: string;
  email?: string;
  password?: string;
}

const LARGO_MINIMO_PASSWORD = 8;
const CORREO_VALIDO = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function validar(nombre: string, email: string, password: string): ErroresCampos {
  const errores: ErroresCampos = {};
  if (!nombre.trim()) errores.nombre = "Escribe tu nombre.";
  else if (nombre.length > 100) errores.nombre = "El nombre es demasiado largo.";

  if (!email.trim()) errores.email = "Escribe tu correo.";
  else if (!CORREO_VALIDO.test(email)) errores.email = "El correo no tiene un formato válido.";

  if (!password) errores.password = "Escribe una contraseña.";
  else if (password.length < LARGO_MINIMO_PASSWORD)
    errores.password = `La contraseña debe tener al menos ${LARGO_MINIMO_PASSWORD} caracteres.`;

  return errores;
}

export default function Registro() {
  const navegar = useNavigate();

  const [nombre, setNombre] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [errores, setErrores] = useState<ErroresCampos>({});
  const [errorGeneral, setErrorGeneral] = useState<string | null>(null);
  const [enviando, setEnviando] = useState(false);

  async function onSubmit(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (enviando) return;

    const nuevos = validar(nombre, email, password);
    setErrores(nuevos);
    setErrorGeneral(null);
    if (nuevos.nombre || nuevos.email || nuevos.password) return;

    setEnviando(true);
    try {
      await register(nombre.trim(), email.trim(), password);
      navegar("/login", { replace: true, state: { registrado: true } });
    } catch (error) {
      if (error instanceof ErrorHttp && error.status === 409) {
        setErrores({ email: "Este correo ya está registrado." });
        setEnviando(false);
        return;
      }
      setErrorGeneral(error instanceof ErrorHttp ? error.message : "Ocurrió un error inesperado.");
      setPassword("");
      setEnviando(false);
    }
  }

  return (
    <section className={`tarjeta ${estilos.contenedor}`}>
      <h1 className="titulo-pagina">Crear cuenta</h1>
      <p className="texto-secundario">Regístrate para comprar tokens y apostar.</p>

      {errorGeneral && (
        <p className={estilos.error} role="alert">
          {errorGeneral}
        </p>
      )}

      <form className={estilos.formulario} onSubmit={onSubmit} noValidate>
        <div className={estilos.campo}>
          <label htmlFor="registro-nombre">Nombre</label>
          <input
            id="registro-nombre"
            type="text"
            autoComplete="name"
            maxLength={100}
            value={nombre}
            onChange={(e) => setNombre(e.target.value)}
            aria-invalid={errores.nombre ? true : undefined}
            aria-describedby={errores.nombre ? "registro-nombre-error" : undefined}
            disabled={enviando}
          />
          {errores.nombre && (
            <span id="registro-nombre-error" className={estilos.errorCampo}>
              {errores.nombre}
            </span>
          )}
        </div>

        <div className={estilos.campo}>
          <label htmlFor="registro-email">Correo</label>
          <input
            id="registro-email"
            type="email"
            autoComplete="email"
            inputMode="email"
            maxLength={255}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-invalid={errores.email ? true : undefined}
            aria-describedby={errores.email ? "registro-email-error" : undefined}
            disabled={enviando}
          />
          {errores.email && (
            <span id="registro-email-error" className={estilos.errorCampo}>
              {errores.email}
            </span>
          )}
        </div>

        <div className={estilos.campo}>
          <label htmlFor="registro-password">Contraseña</label>
          <input
            id="registro-password"
            type="password"
            autoComplete="new-password"
            minLength={LARGO_MINIMO_PASSWORD}
            maxLength={128}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={errores.password ? true : undefined}
            aria-describedby={errores.password ? "registro-password-error" : undefined}
            disabled={enviando}
          />
          {errores.password && (
            <span id="registro-password-error" className={estilos.errorCampo}>
              {errores.password}
            </span>
          )}
        </div>

        <button type="submit" className="boton" disabled={enviando}>
          {enviando ? "Creando cuenta…" : "Registrarme"}
        </button>
      </form>

      <p className="texto-secundario">
        ¿Ya tienes cuenta? <Link to="/login">Inicia sesión</Link>
      </p>
    </section>
  );
}