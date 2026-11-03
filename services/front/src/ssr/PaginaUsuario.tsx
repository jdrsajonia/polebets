// Pagina del usuario (HU2-03, RF-04). Se renderiza DOS veces con los mismos datos:
//
//   1. En el servidor SSR (Node), con renderToString: el HTML llega al
//      navegador ya con el perfil, el saldo, los eventos, las apuestas activas
//      y las ultimas 10 transacciones («Ver código fuente» los muestra).
//   2. En el navegador, con hydrateRoot: React "adopta" ese HTML y le conecta
//      los eventos (Salir, Actualizar) sin volver a pedir los datos.
//
// Por eso este componente no hace peticiones al montar ni lee el reloj: todo
// lo que pinta viene en las props, y el servidor y el navegador producen el
// mismo HTML.
//
// Si una seccion de Pagos llego en null, se muestra su aviso y el resto de la
// pagina sigue (criterio de HU2-03: Pagos caido no rompe la pagina).

import { useState } from "react";
import {
  formatoDelta,
  formatoFecha,
  formatoNumero,
  nombreEstado,
  nombreTorneo,
  nombreTransaccion,
} from "./formato";
import type { ApuestaPagina, EventoPagina, PropsPagina, TransaccionPagina } from "./tipos";

// Rutas de la SPA: se abren con navegacion completa (son otra aplicacion).
const ENLACES = [
  { a: "/carrera", texto: "En vivo" },
  { a: "/billetera", texto: "Billetera" },
  { a: "/mi-pagina", texto: "Mi página", actual: true },
];

export default function PaginaUsuario({ datos, generadaEn }: PropsPagina) {
  const { usuario, saldo, eventos, apuestas_activas: apuestas, transacciones, avisos } = datos;
  const [saliendo, setSaliendo] = useState(false);

  // Cierra la sesion en Usuarios (via el Gateway) y vuelve al ingreso. Si
  // Usuarios no responde, igual se sale: quedarse "medio logueado" es peor.
  async function salir() {
    setSaliendo(true);
    try {
      await fetch("/api/auth/logout", { method: "POST", credentials: "include" });
    } finally {
      window.location.assign("/login");
    }
  }

  return (
    <div className="pu">
      <header className="pu-header">
        <a href="/carrera" className="pu-logo" aria-label="PoleBet, inicio">
          <span className="pu-logo-barra" aria-hidden="true" />
          POLE<span className="pu-logo-acento">BET</span>
        </a>
        <nav className="pu-nav" aria-label="Principal">
          {ENLACES.map((e) => (
            <a key={e.a} href={e.a} className={e.actual ? "pu-enlace pu-enlace-actual" : "pu-enlace"} aria-current={e.actual ? "page" : undefined}>
              {e.texto}
            </a>
          ))}
        </nav>
        <button type="button" className="pu-salir" onClick={salir} disabled={saliendo}>
          {saliendo ? "Saliendo…" : "Salir"}
        </button>
      </header>

      <main className="pu-main">
        <div className="pu-titulo">
          <h1 className="titulo-pagina">Mi página</h1>
          <span className="texto-secundario">
            Actualizada {formatoFecha(generadaEn)} ·{" "}
            <button type="button" className="pu-actualizar" onClick={() => window.location.reload()}>
              Actualizar
            </button>
          </span>
        </div>

        {avisos.length > 0 && (
          <ul className="pu-avisos" role="alert">
            {avisos.map((a) => (
              <li key={a}>{a}</li>
            ))}
          </ul>
        )}

        <div className="pu-rejilla">
          <section className="tarjeta" aria-labelledby="pu-perfil">
            <h2 id="pu-perfil" className="titulo-seccion">Perfil</h2>
            <p className="pu-nombre">{usuario.nombre}</p>
            <p className="texto-secundario">{usuario.email}</p>
            <p className="texto-secundario">Jugador</p>
          </section>

          <section className="tarjeta" aria-labelledby="pu-saldo">
            <h2 id="pu-saldo" className="titulo-seccion">Saldo</h2>
            {saldo === null ? (
              <p className="pu-vacio">No pudimos cargar tu saldo.</p>
            ) : (
              <p className="pu-saldo">
                {formatoNumero(saldo)} <span>PoleCoins</span>
              </p>
            )}
            <a href="/billetera" className="pu-enlace-seccion">Comprar tokens</a>
          </section>
        </div>

        <section className="tarjeta" aria-labelledby="pu-eventos">
          <h2 id="pu-eventos" className="titulo-seccion">Mis eventos</h2>
          <Eventos eventos={eventos} />
        </section>

        <section className="tarjeta" aria-labelledby="pu-apuestas">
          <h2 id="pu-apuestas" className="titulo-seccion">Apuestas activas</h2>
          <Apuestas apuestas={apuestas} />
        </section>

        <section className="tarjeta" aria-labelledby="pu-transacciones">
          <h2 id="pu-transacciones" className="titulo-seccion">Últimas transacciones</h2>
          <Transacciones transacciones={transacciones} />
        </section>
      </main>
    </div>
  );
}

function Eventos({ eventos }: { eventos: EventoPagina[] | null }) {
  if (eventos === null) return <p className="pu-vacio">No pudimos cargar tus eventos.</p>;
  if (eventos.length === 0) return <p className="pu-vacio">Aún no te inscribes a ningún torneo.</p>;
  return (
    <div className="pu-tabla-contenedor">
      <table className="pu-tabla">
        <thead>
          <tr>
            <th scope="col">Carrera</th>
            <th scope="col">Largada</th>
            <th scope="col">Estado</th>
            <th scope="col">Torneo</th>
            <th scope="col" className="pu-num">Puesto</th>
            <th scope="col" className="pu-num">Fichas</th>
            <th scope="col" className="pu-num">Premio</th>
          </tr>
        </thead>
        <tbody>
          {eventos.map((e) => (
            <tr key={e.evento_id}>
              <td>{e.carrera}</td>
              <td>{formatoFecha(e.largada)}</td>
              <td>{nombreEstado(e.estado)}</td>
              <td>{nombreTorneo(e.estado_torneo, e.devuelta)}</td>
              <td className="pu-num">{e.puesto === null ? "—" : `${e.puesto}º`}</td>
              <td className="pu-num">{e.fichas === null ? "—" : formatoNumero(e.fichas)}</td>
              <td className="pu-num">{e.premio === null ? "—" : `${formatoNumero(e.premio)} PoleCoins`}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Apuestas({ apuestas }: { apuestas: ApuestaPagina[] | null }) {
  if (apuestas === null) return <p className="pu-vacio">No pudimos cargar tus apuestas activas.</p>;
  if (apuestas.length === 0) return <p className="pu-vacio">No tienes apuestas pendientes.</p>;
  return (
    <ul className="pu-lista">
      {apuestas.map((a) => {
        const unidad = a.modo === "torneo" ? "fichas" : "PoleCoins";
        return (
          <li key={a.apuesta_id}>
            <span>
              <strong>{a.opcion}</strong> · {a.modo === "torneo" ? "Torneo" : "Individual"} · ×{a.multiplicador}
            </span>
            <span className="pu-num">
              {formatoNumero(a.monto)} {unidad} → {formatoNumero(Math.floor(a.monto * a.multiplicador))} {unidad}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

function Transacciones({ transacciones }: { transacciones: TransaccionPagina[] | null }) {
  if (transacciones === null) return <p className="pu-vacio">No pudimos cargar tus últimas transacciones.</p>;
  if (transacciones.length === 0) return <p className="pu-vacio">Todavía no tienes movimientos.</p>;
  return (
    <ul className="pu-lista">
      {transacciones.map((t, i) => (
        <li key={`${t.creada_en}-${i}`}>
          <span>
            {nombreTransaccion(t.tipo)} <span className="texto-secundario">· {formatoFecha(t.creada_en)}</span>
          </span>
          <span className={t.tokens >= 0 ? "pu-num pu-mas" : "pu-num pu-menos"}>{formatoDelta(t.tokens)} PoleCoins</span>
        </li>
      ))}
    </ul>
  );
}
