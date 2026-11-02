// Package pagina arma los datos de la pagina del usuario (HU2-03, RF-04,
// seccion 3.2 de documentation/orquestador-api.md):
//
//	GET /pagina-usuario   (el Gateway la expone como /api/pagina-usuario)
//
// La llama el servidor SSR del Front reenviando la cookie del jugador; el
// Gateway la valida y agrega X-User-Id / X-User-Rol. Solo jugadores.
//
// Pasos (todos lecturas, en paralelo salvo el 6, que depende del 3):
//
//  1. Usuarios GET /internal/users/{id}                        usuario
//  2. Pagos    GET /internal/wallets/{id}                      saldo
//  3. Pagos    GET /internal/users/{id}/event-entries          eventos
//  4. Pagos    GET /internal/users/{id}/bets?status=pending    apuestas activas
//  5. Pagos    GET /internal/users/{id}/ledger?limit=10        ultimas transacciones
//  6. Motor    GET /internal/torneos/{evento}/jugadores/{id}   fichas y puesto en vivo (torneos activos)
//
// Si falla (el cliente interno ya reintento las lecturas):
//   - Usuarios -> 503: sin perfil no hay pagina.
//   - Pagos    -> 200 con esa seccion en null y un aviso. La pagina no se rompe.
//   - Motor    -> 200 con fichas y puesto en null en esos eventos y un aviso.
package pagina

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
	"github.com/arquisoft/orquestador/internal/sesion"
)

// Servicio es lo que este flujo necesita de un cliente interno (interno.Cliente lo cumple).
type Servicio interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// Servicios agrupa los tres clientes que usa la pagina.
type Servicios struct {
	Usuarios, Pagos, Motor Servicio
}

// Avisos que ve el jugador cuando una parte no cargo.
const (
	AvisoSaldo         = "No pudimos cargar tu saldo. Intenta de nuevo en un momento."
	AvisoEventos       = "No pudimos cargar tus eventos."
	AvisoApuestas      = "No pudimos cargar tus apuestas activas."
	AvisoTransacciones = "No pudimos cargar tus últimas transacciones."
	AvisoFichas        = "No pudimos cargar tus fichas en vivo."
)

// Estados de torneo que importan aqui (events.tournament_status).
const (
	torneoActivo    = "activo"
	torneoCancelado = "cancelado"
	torneoRepartido = "repartido"
)

// ---------------------------------------------------------------------------
// API publica
// ---------------------------------------------------------------------------

type Usuario struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	Email  string `json:"email"`
	Rol    string `json:"rol"`
}

type Evento struct {
	EventoID     string `json:"evento_id"`
	Carrera      string `json:"carrera"`
	Estado       string `json:"estado"`
	EstadoTorneo string `json:"estado_torneo"`
	Largada      string `json:"largada"`
	Devuelta     bool   `json:"devuelta"`
	Fichas       *int64 `json:"fichas"`
	Puesto       *int64 `json:"puesto"`
	Premio       *int64 `json:"premio"`
}

type Apuesta struct {
	ApuestaID     string  `json:"apuesta_id"`
	MercadoID     string  `json:"mercado_id"`
	EventoID      string  `json:"evento_id"`
	Opcion        string  `json:"opcion"`
	Monto         int64   `json:"monto"`
	Modo          string  `json:"modo"`
	Multiplicador float64 `json:"multiplicador"`
}

type Transaccion struct {
	Tipo     string  `json:"tipo"`
	Tokens   int64   `json:"tokens"`
	EventoID *string `json:"evento_id"`
	CreadaEn string  `json:"creada_en"`
}

// Pagina es la respuesta de GET /pagina-usuario. Las secciones de Pagos son
// punteros/slices nil cuando no cargaron, y salen como null en el JSON.
type Pagina struct {
	Usuario         Usuario        `json:"usuario"`
	Saldo           *int64         `json:"saldo"`
	Eventos         *[]Evento      `json:"eventos"`
	ApuestasActivas *[]Apuesta     `json:"apuestas_activas"`
	Transacciones   *[]Transaccion `json:"transacciones"`
	Avisos          []string       `json:"avisos"`
}

// ---------------------------------------------------------------------------
// Respuestas internas
// ---------------------------------------------------------------------------

type walletPagos struct {
	Balance int64 `json:"balance"`
}

type entradaPagos struct {
	EventID          string `json:"event_id"`
	RaceKey          string `json:"race_key"`
	Status           string `json:"status"`
	TournamentStatus string `json:"tournament_status"`
	StartsAt         string `json:"starts_at"`
	ChipsFinal       *int64 `json:"chips_final"`
	PositionFinal    *int64 `json:"position_final"`
	Prize            *int64 `json:"prize"`
}

type apuestaPagos struct {
	BetID      string  `json:"bet_id"`
	MarketID   string  `json:"market_id"`
	EventID    string  `json:"event_id"`
	Option     string  `json:"option"`
	Amount     int64   `json:"amount"`
	Mode       string  `json:"mode"`
	Multiplier float64 `json:"multiplier"`
}

type movimientoPagos struct {
	Type        string  `json:"type"`
	TokensDelta int64   `json:"tokens_delta"`
	EventID     *string `json:"event_id"`
	CreatedAt   string  `json:"created_at"`
}

type jugadorMotor struct {
	Fichas *int64 `json:"fichas"`
	Puesto *int64 `json:"puesto"`
}

// ---------------------------------------------------------------------------
// Ruta y flujo
// ---------------------------------------------------------------------------

// Register agrega la ruta, protegida con el middleware de sesion.
func Register(mux *http.ServeMux, s Servicios) {
	mux.Handle("GET /pagina-usuario", sesion.ExigirJugador(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			userID, _ := sesion.UsuarioDe(r.Context())
			p, err := Armar(r.Context(), s, userID)
			if err != nil {
				errores.Responder(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store") // datos personales y cambiantes
			httpx.EscribirJSON(w, http.StatusOK, p)
		},
	)))
}

// Armar pide todo en paralelo y junta una sola respuesta.
func Armar(ctx context.Context, s Servicios, userID string) (Pagina, error) {
	var (
		wg                        sync.WaitGroup
		usuario                   Usuario
		errUsuario                error
		saldo                     *int64
		entradas                  []entradaPagos
		apuestas                  []apuestaPagos
		movs                      []movimientoPagos
		okSaldo, okEnt, okAp, okM bool
	)
	id := url.PathEscape(userID)

	wg.Add(5)
	go func() {
		defer wg.Done()
		errUsuario = leerUsuario(ctx, s.Usuarios, id, &usuario)
	}()
	go func() {
		defer wg.Done()
		var w walletPagos
		if okSaldo = leer(ctx, s.Pagos, "/internal/wallets/"+id, &w); okSaldo {
			saldo = &w.Balance
		}
	}()
	go func() {
		defer wg.Done()
		okEnt = leer(ctx, s.Pagos, "/internal/users/"+id+"/event-entries", &entradas)
	}()
	go func() {
		defer wg.Done()
		okAp = leer(ctx, s.Pagos, "/internal/users/"+id+"/bets?status=pending", &apuestas)
	}()
	go func() {
		defer wg.Done()
		okM = leer(ctx, s.Pagos, "/internal/users/"+id+"/ledger?limit=10", &movs)
	}()
	wg.Wait()

	if errUsuario != nil {
		return Pagina{}, errUsuario
	}

	p := Pagina{Usuario: usuario, Saldo: saldo, Avisos: []string{}}
	if !okSaldo {
		p.Avisos = append(p.Avisos, AvisoSaldo)
	}
	if okEnt {
		eventos, fichasOK := armarEventos(ctx, s.Motor, id, entradas)
		p.Eventos = &eventos
		if !fichasOK {
			p.Avisos = append(p.Avisos, AvisoFichas)
		}
	} else {
		p.Avisos = append(p.Avisos, AvisoEventos)
	}
	if okAp {
		lista := make([]Apuesta, 0, len(apuestas))
		for _, a := range apuestas {
			lista = append(lista, Apuesta{a.BetID, a.MarketID, a.EventID, a.Option, a.Amount, a.Mode, a.Multiplier})
		}
		p.ApuestasActivas = &lista
	} else {
		p.Avisos = append(p.Avisos, AvisoApuestas)
	}
	if okM {
		lista := make([]Transaccion, 0, len(movs))
		for _, m := range movs {
			lista = append(lista, Transaccion{m.Type, m.TokensDelta, m.EventID, m.CreatedAt})
		}
		p.Transacciones = &lista
	} else {
		p.Avisos = append(p.Avisos, AvisoTransacciones)
	}
	return p, nil
}

// armarEventos traduce las inscripciones y, para los torneos activos, pide al
// Motor (en paralelo) las fichas y el puesto en vivo. Devuelve false si alguna
// consulta al Motor fallo.
func armarEventos(ctx context.Context, motor Servicio, id string, entradas []entradaPagos) ([]Evento, bool) {
	eventos := make([]Evento, len(entradas))
	fallos := make([]bool, len(entradas))
	var wg sync.WaitGroup
	for i, e := range entradas {
		eventos[i] = Evento{
			EventoID: e.EventID, Carrera: e.RaceKey, Estado: e.Status, EstadoTorneo: e.TournamentStatus,
			Largada: e.StartsAt, Devuelta: e.TournamentStatus == torneoCancelado, Premio: e.Prize,
		}
		switch e.TournamentStatus {
		case torneoRepartido: // resultado final guardado por el cierre (HU2-12)
			eventos[i].Fichas, eventos[i].Puesto = e.ChipsFinal, e.PositionFinal
		case torneoActivo: // en vivo, desde Redis por el Motor
			wg.Add(1)
			go func(i int, eventoID string) {
				defer wg.Done()
				var j jugadorMotor
				ruta := "/internal/torneos/" + url.PathEscape(eventoID) + "/jugadores/" + id
				if leer(ctx, motor, ruta, &j) {
					eventos[i].Fichas, eventos[i].Puesto = j.Fichas, j.Puesto
				} else {
					fallos[i] = true
				}
			}(i, e.EventID)
		}
	}
	wg.Wait()
	for _, f := range fallos {
		if f {
			return eventos, false
		}
	}
	return eventos, true
}

func leerUsuario(ctx context.Context, usuarios Servicio, id string, u *Usuario) error {
	resp, err := usuarios.Hacer(ctx, interno.Peticion{Metodo: http.MethodGet, Ruta: "/internal/users/" + id})
	if err != nil {
		return err // ErrNoDisponible -> 503
	}
	switch resp.Estado {
	case http.StatusOK:
		if err := json.Unmarshal(resp.Cuerpo, u); err != nil {
			return fmt.Errorf("respuesta invalida de Usuarios: %w", err)
		}
		return nil
	case http.StatusNotFound:
		// La cuenta ya no existe: la sesion no sirve.
		return errores.SinSesion
	default:
		return fmt.Errorf("usuarios respondio %d al leer el perfil: %s", resp.Estado, resp.Cuerpo)
	}
}

// leer hace un GET y decodifica un 200. Cualquier otra cosa se registra en el
// log y devuelve false: la seccion sale en null con su aviso.
func leer(ctx context.Context, s Servicio, ruta string, destino any) bool {
	resp, err := s.Hacer(ctx, interno.Peticion{Metodo: http.MethodGet, Ruta: ruta})
	if err != nil {
		log.Printf("pagina-usuario: %s: %v", ruta, err)
		return false
	}
	if resp.Estado != http.StatusOK {
		log.Printf("pagina-usuario: %s respondio %d", ruta, resp.Estado)
		return false
	}
	if err := json.Unmarshal(resp.Cuerpo, destino); err != nil {
		log.Printf("pagina-usuario: %s: respuesta invalida: %v", ruta, err)
		return false
	}
	return true
}
