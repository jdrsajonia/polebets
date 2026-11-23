// Package auth verifica la identidad firmada que emite Usuarios.
//
// El motor no le pregunta a Usuarios en cada peticion: verifica la firma por
// su cuenta con la llave publica, asi el flujo en vivo no depende de otro
// servicio. El resto del motor usa la interfaz ValidarToken, de modo que el
// cambio de HS256 a Ed25519 (escenario 4) no toco los demas paquetes.
package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrSinSesion: la peticion no trae la identidad firmada.
	ErrSinSesion = errors.New("no hay sesion")
	// ErrTokenInvalido: el token esta vencido, mal firmado o mal formado.
	ErrTokenInvalido = errors.New("token invalido")
)

// Sesion es la identidad que se saca de un token valido.
type Sesion struct {
	UserID string
	Rol    string
}

// ValidarToken es lo que el resto del motor necesita de la autenticacion.
type ValidarToken interface {
	Validar(token string) (Sesion, error)
}

var formatoUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidadorEd25519 verifica tokens firmados con la llave privada de Usuarios.
type ValidadorEd25519 struct {
	llave  ed25519.PublicKey
	parser *jwt.Parser
}

// NuevoValidador recibe la llave publica de Usuarios.
func NuevoValidador(llave ed25519.PublicKey) (*ValidadorEd25519, error) {
	if len(llave) != ed25519.PublicKeySize {
		return nil, errors.New("la llave publica de identidad no es una llave Ed25519 valida")
	}
	return &ValidadorEd25519{
		llave: llave,
		parser: jwt.NewParser(
			// Fijar el algoritmo evita que pase un token con "alg": "none" o
			// uno HS256 firmado con un secreto cualquiera.
			jwt.WithValidMethods([]string{Algoritmo}),
			// Un token sin exp nunca venceria: se exige.
			jwt.WithExpirationRequired(),
			// Solo vale lo que emitio Usuarios para los servicios de PoleBet.
			jwt.WithIssuer(Emisor),
			jwt.WithAudience(Audiencia),
		),
	}, nil
}

// LeerLlavePublica carga la llave publica Ed25519 (PEM) del archivo dado.
func LeerLlavePublica(ruta string) (ed25519.PublicKey, error) {
	pem, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la llave publica de identidad: %w", err)
	}
	llave, err := jwt.ParseEdPublicKeyFromPEM(pem)
	if err != nil {
		return nil, fmt.Errorf("la llave de %s no es una llave publica Ed25519: %w", ruta, err)
	}
	publica, ok := llave.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("la llave de %s no es una llave publica Ed25519", ruta)
	}
	return publica, nil
}

// Validar revisa firma, algoritmo, vencimiento, emisor y audiencia, y
// devuelve el user_id (en minusculas) y el rol. Cualquier error envuelve
// ErrTokenInvalido.
func (v *ValidadorEd25519) Validar(token string) (Sesion, error) {
	claims := jwt.MapClaims{}
	if _, err := v.parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return v.llave, nil
	}); err != nil {
		return Sesion{}, fmt.Errorf("%w: %w", ErrTokenInvalido, err)
	}

	userID, _ := claims[ClaimUserID].(string)
	if !formatoUUID.MatchString(userID) {
		return Sesion{}, fmt.Errorf("%w: el claim %q no es un UUID", ErrTokenInvalido, ClaimUserID)
	}
	rol, _ := claims[ClaimRol].(string)
	if rol != RolJugador && rol != RolAdmin {
		return Sesion{}, fmt.Errorf("%w: rol %q desconocido", ErrTokenInvalido, rol)
	}
	return Sesion{UserID: strings.ToLower(userID), Rol: rol}, nil
}

// DesdeRequest lee la identidad firmada que puso el Gateway y la valida.
func DesdeRequest(v ValidarToken, r *http.Request) (Sesion, error) {
	token := strings.TrimSpace(r.Header.Get(HeaderIdentidad))
	if token == "" {
		return Sesion{}, ErrSinSesion
	}
	return v.Validar(token)
}
