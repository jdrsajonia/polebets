package auth

// Contrato de la identidad firmada con Usuarios (escenario 4, T3-05).
//
// Usuarios firma, en cada auth_request del Gateway, un token corto con el id
// y el rol del usuario, usando su llave PRIVADA Ed25519. El Gateway lo reenvia
// en X-User-Token y el motor lo verifica con la llave PUBLICA. El motor ya no
// tiene JWT_SECRET: con HS256, quien verifica tambien puede firmar, y un motor
// comprometido podia fabricar sesiones de cualquier usuario.
//
// El emisor esta en services/usuarios/app/core/identidad.py; el Orquestador
// verifica el mismo token en internal/sesion. Si algo cambia, cambia en los tres.
const (
	// Algoritmo de firma. Cualquier otro (incluidos "none" y HS256) se rechaza.
	Algoritmo = "EdDSA"

	// HeaderIdentidad es donde el Gateway reenvia el token.
	HeaderIdentidad = "X-User-Token"

	// Emisor y Audiencia que debe traer el token ("iss" y "aud").
	Emisor    = "usuarios"
	Audiencia = "polebet"

	// ClaimUserID es el id del usuario: un UUID como string.
	ClaimUserID = "sub"
	// ClaimRol es el rol del usuario; debe ser RolJugador o RolAdmin.
	ClaimRol = "rol"

	// Valores del rol, los de users.role en Usuarios.
	RolJugador = "player"
	RolAdmin   = "admin"
)
