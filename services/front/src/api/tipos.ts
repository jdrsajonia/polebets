// Tipos de los mensajes y respuestas que el front intercambia con los backends.
// Si un contrato cambia, se cambia solo aqui.

// ---------------------------------------------------------------------------
// WebSocket del motor
// ---------------------------------------------------------------------------

// Sobre comun de todo lo que el motor envia por WebSocket
// (services/motor/internal/ws/mensajes.go).
export interface MensajeWs<T = unknown> {
  type: string;
  payload: T;
}

// Tipos de mensaje acordados con el motor.
export const TIPOS_WS = {
  posiciones: "posiciones", // HU-05
  mercadoAbierto: "mercado_abierto", // HU-11
  mercadoCerrado: "mercado_cerrado", // HU-11
  mercadoLiquidado: "mercado_liquidado", // HU-07
  resultadoApuesta: "resultado_apuesta", // HU-08
  rankingTorneo: "ranking_torneo", // HU2-11
  mercadoDinamico: "mercado_dinamico", // HU2-08
} as const;

// ---------------------------------------------------------------------------
// HU-05: posiciones en vivo
// ---------------------------------------------------------------------------
// Contrato con el motor (services/motor/README.md, "Posiciones en vivo").

export interface Piloto {
  posicion: number;
  numero: number; // numero del carro; identifica al piloto entre fotos
  codigo: string; // tres letras, p. ej. "VAR"
  nombre: string;
  equipo: string;
  color_equipo?: string; // color hex del equipo (opcional)
  diferencia: string; // con el lider: "Líder" para P1; el motor manda "—" al resto (no hay intervalos)
}

// Foto de las posiciones en un instante. Es el payload del mensaje
// "posiciones" y tambien la respuesta de GET /posiciones.
export interface FotoPosiciones {
  carrera_id: string;
  vuelta?: number; // vuelta actual (HU2-14, de simulaciones.vuelta_actual)
  total_vueltas?: number; // total de vueltas de la carrera (HU2-14)
  actualizado_en: string; // ISO: cuando cambio el dato en la carrera
  emitido_en?: string; // ISO: cuando el motor envio el mensaje (latencia, RNF-04)
  pilotos: Piloto[];
}

// ---------------------------------------------------------------------------
// HU-11: mercados
// ---------------------------------------------------------------------------
// Payload de "mercado_abierto" y "mercado_cerrado", y respuesta de
// GET /mercados/actual y POST /mercados (services/motor/README.md).

export type EstadoMercado = "abierto" | "cerrado" | "liquidado";

export interface Mercado {
  id: string;
  carrera_id: string;
  pregunta: string;
  opciones: string[];
  estado: EstadoMercado;
  abre_en: string; // ISO
  cierra_en: string; // ISO: fin de la ventana de apuestas
  observa_hasta: string; // ISO: fin de la ventana de observacion
  ahora: string; // ISO: hora del servidor al enviar (para el desfase del reloj)
}

// Lo que envia el admin para abrir un mercado.
export interface PeticionMercado {
  pregunta: string;
  opciones: string[];
  carrera_id?: string; // sin el, el motor usa la ultima carrera cargada
}

// ---------------------------------------------------------------------------
// HU-08: resultado de la apuesta
// ---------------------------------------------------------------------------
// Payload de "resultado_apuesta": el motor lo envia solo al dueño de la
// apuesta al liquidarla (services/motor/README.md). No trae saldo: los tokens
// apostados no se devuelven y la liquidacion no lo cambia.

export interface ResultadoApuesta {
  mercado_id: string;
  apuesta_id: string;
  opcion: string; // lo que aposto el jugador
  opcion_ganadora: string;
  gano: boolean;
  puntos_obtenidos: number; // 10 + monto x 0,5 si gano; puede tener decimales
  puntaje_total: number; // puntos acumulados del jugador (points_total de Pagos)
  // HU2-11: solo presentes si la apuesta fue en modo torneo. En ese caso el
  // front muestra fichas (no puntos) y el nuevo puesto (RF-17).
  modo?: "torneo";
  fichas_ganadas?: number; // fichas que pago esta apuesta (0 si perdio)
  fichas_total?: number; // saldo de fichas del jugador tras liquidar
  puesto?: number; // puesto del jugador en el ranking tras liquidar
}

// ---------------------------------------------------------------------------
// HU2-11: fichas y ranking del torneo
// ---------------------------------------------------------------------------
// Payload del mensaje "ranking_torneo": el motor lo difunde a todos cuando
// cambian las fichas de un torneo (services/motor/internal/fichas). El front
// lo usa para la tabla del ranking y la barra de avance hacia el minimo
// apostado (RF-18, RNF-13: cambia en menos de 1 s).

export interface FilaRanking {
  puesto: number; // los empatados comparten numero y el siguiente grupo sigue (1,1,2)
  usuario_id: string;
  fichas: number; // saldo actual de fichas
  apostado: number; // fichas comprometidas en apuestas (base de la barra)
  avance_pct: number; // 0..100, apostado frente al minimo para ser elegible
  elegible: boolean; // ya aposto el minimo para entrar al reparto (HU2-12)
}

export interface RankingTorneo {
  evento_id: string;
  filas: FilaRanking[];
  min_apostado: number; // fichas que hay que apostar para ser elegible
  botin: number; // pot del evento (informativo)
  actualizado_en: string; // ISO: cuando cambio el dato
  emitido_en?: string; // ISO: cuando el motor lo envio (latencia, RNF-13)
}

// ---------------------------------------------------------------------------
// HU2-08: apuestas dinamicas
// ---------------------------------------------------------------------------
// Payload de "mercado_dinamico": el Gestor del motor lo difunde al abrir,
// cerrar y resolver un mercado dinamico (services/motor/README.md, "Apuestas
// dinamicas"). Convive con mercado_abierto/mercado_cerrado del P1.

export interface OpcionDinamica {
  opcion: string; // "Sí" o "No"
  multiplicador: number;
}

export interface MensajeDinamico {
  market_id: string;
  event_id: string; // _id de la simulacion (= evento en Pagos)
  template: string; // adelantamiento, cambio_lider, ...
  question: string;
  options: OpcionDinamica[];
  status: "abierto" | "cerrado";
  race_close_s: number; // segundo de carrera del cierre
  closes_at: string; // ISO UTC: solo para la cuenta regresiva (decide el motor)
  winning_option: string | null; // solo al resolver
}

// POST /api/apuestas (documentation/orquestador-api.md §3.6).
export type ModoApuesta = "individual" | "torneo";

export interface PeticionApuesta {
  mercado_id: string;
  opcion: string;
  monto: number; // entero > 0
}

export interface RespuestaApuesta {
  apuesta_id: string;
  mercado_id: string;
  opcion: string;
  monto: number;
  modo: ModoApuesta;
  multiplicador: number;
  pago_si_acierta: number; // en tokens o fichas segun el modo
  saldo: number | null; // tokens que quedan (individual)
  fichas: number | null; // fichas que quedan (torneo)
}

// Payload de "resultado_apuesta" de los mercados dinamicos (HU2-10, HU2-13):
// lo envia el worker de liquidar del motor solo al dueño de la apuesta
// (services/motor/internal/liquidar, structs Resultado y ResultadoTorneo).
// No trae puntos: el del P1 sigue siendo ResultadoApuesta.

interface ResultadoDinamicoBase {
  mercado_id: string;
  apuesta_id: string;
  pregunta: string;
  opcion: string; // lo que aposto el jugador
  opcion_ganadora: string;
  gano: boolean;
  monto: number;
  multiplicador: number;
}

export interface ResultadoIndividual extends ResultadoDinamicoBase {
  modo: "individual";
  pago: number; // PoleCoins recibidos (0 si perdio)
  saldo: number; // saldo tras liquidar
}

export interface ResultadoTorneo extends ResultadoDinamicoBase {
  modo: "torneo";
  fichas_ganadas: number; // 0 si perdio
  fichas_perdidas: number; // el monto si perdio, 0 si gano
  fichas_total: number; // fichas del jugador tras liquidar
  puesto: number; // puesto en el ranking tras liquidar
}

export type ResultadoDinamico = ResultadoIndividual | ResultadoTorneo;

// GET /api/eventos/{evento_id} (§3.4): solo lo que el front usa para
// mostrar el modo antes de confirmar (RF-09).
export interface DetalleEvento {
  evento: { evento_id: string; estado: string; estado_torneo: string };
  mi_inscripcion: { inscrito: boolean; devuelta: boolean };
}

// ---------------------------------------------------------------------------
// Errores
// ---------------------------------------------------------------------------

// Forma unica de los errores de http.ts. status 0 = no hubo respuesta.
// codigo es el identificador estable del Orquestador ({ error, codigo }).
export interface ErrorApi {
  status: number;
  message: string;
  codigo?: string;
}
