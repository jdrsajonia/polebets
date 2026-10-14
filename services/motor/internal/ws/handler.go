package ws

import (
	"log"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

// mismoOrigen es true si el header Origin apunta al mismo host:puerto por el
// que llego la peticion (r.Host). Detras del proxy del front (Nginx reenvia
// Origin y Host sin tocarlos) el WebSocket es same-origin, asi que se acepta
// sin tener que listar cada IP o dominio en ALLOWED_ORIGINS. Para accesos
// directos al motor desde otro origen sigue mandando la allowlist.
func mismoOrigen(r *http.Request) bool {
	origen := r.Header.Get("Origin")
	if origen == "" {
		return false
	}
	u, err := url.Parse(origen)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// Identificador obtiene el user_id de quien abre la conexion. La
// implementacion real valida el JWT de la cookie de sesion (RNF-06); si
// devuelve error la conexion se rechaza con 401.
type Identificador interface {
	Identificar(r *http.Request) (userID string, err error)
}

// IdentificadorFunc adapta una funcion a Identificador.
type IdentificadorFunc func(r *http.Request) (string, error)

func (f IdentificadorFunc) Identificar(r *http.Request) (string, error) { return f(r) }

// Register monta GET /ws. origenPermitido decide que paginas pueden abrir el
// WebSocket: como la sesion viaja en una cookie, sin este filtro cualquier
// sitio podria abrir una conexion a nombre del usuario.
func Register(mux *http.ServeMux, hub *Hub, id Identificador, origenPermitido func(origen string) bool) {
	mux.Handle("GET /ws", Handler(hub, id, origenPermitido))
}

func Handler(hub *Hub, id Identificador, origenPermitido func(origen string) bool) http.Handler {
	revisarOrigen := func(r *http.Request) bool {
		// Clientes que no son navegador (pruebas, wscat) no envian Origin.
		origen := r.Header.Get("Origin")
		return origen == "" || mismoOrigen(r) || origenPermitido(origen)
	}
	upgrader := websocket.Upgrader{CheckOrigin: revisarOrigen}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !revisarOrigen(r) {
			http.Error(w, "origen no permitido", http.StatusForbidden)
			return
		}
		// Sin user_id no se le podria enviar su resultado (HU-08): se rechaza
		// igual que una sesion invalida.
		userID, err := id.Identificar(r)
		if err != nil || userID == "" {
			http.Error(w, "no autorizado", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// Upgrade ya respondio al cliente con el error.
			log.Printf("error al abrir WebSocket: %v", err)
			return
		}
		c := &cliente{hub: hub, conn: conn, userID: userID, send: make(chan []byte, tamBufferEnvio)}
		if err := hub.entrar(c); err != nil {
			_ = conn.Close()
			return
		}
		go c.writePump()
		go c.readPump()
	})
}
