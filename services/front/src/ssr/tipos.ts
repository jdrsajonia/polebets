// Datos de la pagina del usuario (HU2-03): la respuesta de GET /api/pagina-usuario
// del Orquestador (documentation/orquestador-api.md, §3.2). Las secciones de
// Pagos llegan en null si Pagos no respondio; `avisos` dice que falto.

export interface UsuarioPagina {
  id: string;
  nombre: string;
  email: string;
  rol: "player" | "admin";
}

export interface EventoPagina {
  evento_id: string;
  carrera: string;
  estado: string; // programado | en_curso | finalizado
  estado_torneo: string; // abierto | activo | cancelado | repartido
  largada: string; // ISO 8601
  devuelta: boolean;
  fichas: number | null;
  puesto: number | null;
  premio: number | null;
}

export interface ApuestaPagina {
  apuesta_id: string;
  mercado_id: string;
  evento_id: string;
  opcion: string;
  monto: number;
  modo: "individual" | "torneo" | string;
  multiplicador: number;
}

export interface TransaccionPagina {
  tipo: string; // purchase | debit | settle | buy_in | refund | prize
  tokens: number;
  evento_id: string | null;
  creada_en: string; // ISO 8601
}

export interface DatosPagina {
  usuario: UsuarioPagina;
  saldo: number | null;
  eventos: EventoPagina[] | null;
  apuestas_activas: ApuestaPagina[] | null;
  transacciones: TransaccionPagina[] | null;
  avisos: string[];
}

// Lo que el servidor SSR le pasa al componente: los datos y cuando se armaron.
export interface PropsPagina {
  datos: DatosPagina;
  generadaEn: string; // ISO 8601, lo fija el servidor para que la hidratacion coincida
}
