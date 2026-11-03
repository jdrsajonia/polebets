package dinamicas

// Mercado es un mercado dinamico abierto por el Gestor.
type Mercado struct {
	Propuesta
	ID       string // UUIDv5 determinista: clave de idempotencia en Pagos
	EventoID string // _id de la simulacion (acordado con HU2-07)

	T0         int64 // t_carrera_ms en que abre
	Cierre     int64 // race_close = T0 + ventana
	Resolucion int64 // Cierre + observacion

	// OrdenCierre es la clasificacion en Cierre (entrada_pits la usa).
	OrdenCierre []int

	obs []Evento // eventos en (Cierre, Resolucion]
}
