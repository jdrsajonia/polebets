// Package registro implementa el registro orquestado (HU2-01, RF-01, seccion
// 3.1 de documentation/orquestador-api.md):
//
//	POST /registro  {nombre, email, contrasena}  -> 201 {usuario}
//
// Es una ruta SIN sesion: el Gateway la deja pasar sin auth_request. No abre
// sesion; despues del registro el front hace login (Gateway -> Usuarios).
//
// Pasos:
//
//  0. El orquestador genera el user_id (uuid v4).
//  1. Pagos    POST /internal/wallets {user_id}   Idempotency-Key: wallet:{id}
//  2. Usuarios POST /internal/users {id, nombre, email, contrasena}
//     Idempotency-Key: user:{id}
//
// La billetera va primero: asi nunca queda una cuenta sin billetera (RF-01).
// Los dos pasos son idempotentes, por eso el cliente interno los reintenta
// con la misma clave ante red caida o 502/503/504. Si el paso 2 no termina,
// queda una billetera en 0 de un id que nadie usa: es inofensiva (nadie puede
// iniciar sesion con ese id ni comprarle tokens) y el contrato la acepta.
package registro

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/arquisoft/orquestador/internal/errores"
	"github.com/arquisoft/orquestador/internal/httpx"
	"github.com/arquisoft/orquestador/internal/interno"
)

// TamanoMaximoCuerpo limita el JSON de entrada (nombre + correo + clave caben de sobra).
const TamanoMaximoCuerpo = 4 << 10

// Servicio es lo que este flujo necesita de un cliente interno (interno.Cliente
// lo cumple). Es una interfaz para probar el flujo sin red.
type Servicio interface {
	Hacer(ctx context.Context, p interno.Peticion) (interno.Respuesta, error)
}

// Usuario es el objeto `usuario` de la API publica (el mismo del P1).
type Usuario struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	Email  string `json:"email"`
	Rol    string `json:"rol"`
}

// Respuesta es el cuerpo publico del 201.
type Respuesta struct {
	Usuario Usuario `json:"usuario"`
}

// Datos es el cuerpo publico de POST /registro. La forma la valida el
// orquestador; el formato del correo y el largo de la clave los decide
// Usuarios (su 422 se traduce a cuerpo_invalido).
type Datos struct {
	Nombre     string `json:"nombre"`
	Email      string `json:"email"`
	Contrasena string `json:"contrasena"`
}

// ClaveBilletera y ClaveUsuario son las claves de idempotencia de cada paso.
func ClaveBilletera(userID string) string { return "wallet:" + userID }
func ClaveUsuario(userID string) string   { return "user:" + userID }

// Register agrega la ruta. No lleva sesion.ExigirJugador: es publica.
func Register(mux *http.ServeMux, pagos, usuarios Servicio) {
	mux.HandleFunc("POST /registro", func(w http.ResponseWriter, r *http.Request) {
		datos, ok := leerDatos(w, r)
		if !ok {
			errores.Responder(w, errores.CuerpoInvalido)
			return
		}
		u, err := Registrar(r.Context(), pagos, usuarios, NuevoID(), datos)
		if err != nil {
			errores.Responder(w, err)
			return
		}
		httpx.EscribirJSON(w, http.StatusCreated, Respuesta{Usuario: u})
	})
}

// leerDatos exige los tres campos como texto no vacio y rechaza campos
// desconocidos (seccion 2 del contrato: cuerpo_invalido).
func leerDatos(w http.ResponseWriter, r *http.Request) (Datos, bool) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, TamanoMaximoCuerpo))
	dec.DisallowUnknownFields()
	var d Datos
	if err := dec.Decode(&d); err != nil || dec.More() {
		return Datos{}, false
	}
	d.Nombre = strings.TrimSpace(d.Nombre)
	d.Email = strings.TrimSpace(d.Email)
	if d.Nombre == "" || d.Email == "" || d.Contrasena == "" {
		return Datos{}, false
	}
	return d, true
}

// Registrar ejecuta los dos pasos con el id dado y devuelve el usuario creado.
func Registrar(ctx context.Context, pagos, usuarios Servicio, userID string, d Datos) (Usuario, error) {
	// Paso 1: billetera con saldo 0 en Pagos (201 creada, 200 ya existia).
	resp, err := pagos.Hacer(ctx, interno.Peticion{
		Metodo:         http.MethodPost,
		Ruta:           "/internal/wallets",
		Cuerpo:         map[string]string{"user_id": userID},
		IdempotencyKey: ClaveBilletera(userID),
	})
	if err != nil {
		return Usuario{}, err // ErrNoDisponible -> 503 y no se crea nada en Usuarios
	}
	if resp.Estado != http.StatusCreated && resp.Estado != http.StatusOK {
		return Usuario{}, fmt.Errorf("pagos respondio %d al crear la billetera: %s", resp.Estado, resp.Cuerpo)
	}

	// Paso 2: la cuenta en Usuarios con el MISMO id.
	resp, err = usuarios.Hacer(ctx, interno.Peticion{
		Metodo: http.MethodPost,
		Ruta:   "/internal/users",
		Cuerpo: map[string]string{
			"id": userID, "nombre": d.Nombre, "email": d.Email, "contrasena": d.Contrasena,
		},
		IdempotencyKey: ClaveUsuario(userID),
	})
	if err != nil {
		return Usuario{}, err
	}
	switch resp.Estado {
	case http.StatusCreated, http.StatusOK:
		var u Usuario
		if err := json.Unmarshal(resp.Cuerpo, &u); err != nil {
			return Usuario{}, fmt.Errorf("respuesta invalida de Usuarios: %w", err)
		}
		return u, nil
	case http.StatusConflict:
		return Usuario{}, errores.CorreoRegistrado
	case http.StatusUnprocessableEntity:
		// Correo mal formado, contraseña corta, nombre largo...
		return Usuario{}, errores.CuerpoInvalido
	default:
		// 400 (clave mal armada) o 401 (token) son errores nuestros, no del visitante.
		return Usuario{}, fmt.Errorf("usuarios respondio %d al crear la cuenta: %s", resp.Estado, bytes.TrimSpace(resp.Cuerpo))
	}
}

// NuevoID genera un uuid v4 (RFC 4122) con crypto/rand, sin dependencias.
func NuevoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // sin entropia del sistema no se puede continuar
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
