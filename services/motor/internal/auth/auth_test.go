package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const userPrueba = "3f2b8c1e-9a4d-4e6f-8b1a-2c3d4e5f6a7b"

// Par Ed25519 de prueba: hace el papel de la llave de Usuarios.
var publicaPrueba, privadaPrueba, _ = ed25519.GenerateKey(rand.Reader)

// firmar crea un token EdDSA como lo emitiria Usuarios.
func firmar(t *testing.T, llave ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(llave)
	if err != nil {
		t.Fatalf("no se pudo firmar: %v", err)
	}
	return token
}

func claimsValidos() jwt.MapClaims {
	return jwt.MapClaims{
		ClaimUserID: userPrueba,
		ClaimRol:    RolJugador,
		"iss":       Emisor,
		"aud":       Audiencia,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Minute).Unix(),
	}
}

func validador(t *testing.T) *ValidadorEd25519 {
	t.Helper()
	v, err := NuevoValidador(publicaPrueba)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTokenValido(t *testing.T) {
	token := firmar(t, privadaPrueba, claimsValidos())

	s, err := validador(t).Validar(token)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if s.UserID != userPrueba || s.Rol != RolJugador {
		t.Errorf("sesion = %+v", s)
	}
}

func TestTokensRechazados(t *testing.T) {
	con := func(cambiar func(jwt.MapClaims)) jwt.MapClaims {
		c := claimsValidos()
		cambiar(c)
		return c
	}
	_, otraPrivada, _ := ed25519.GenerateKey(rand.Reader)
	hs256, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsValidos()).SignedString([]byte("secreto-cualquiera"))

	// Cuerpo de otro usuario con la firma de un token real: la firma no coincide.
	partes := strings.Split(firmar(t, privadaPrueba, claimsValidos()), ".")
	otro := strings.Split(firmar(t, privadaPrueba, con(func(c jwt.MapClaims) {
		c[ClaimUserID] = "00000000-0000-0000-0000-000000000009"
	})), ".")
	alterado := partes[0] + "." + otro[1] + "." + partes[2]

	casos := []struct {
		nombre string
		token  string
	}{
		{"vencido", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Second).Unix() }))},
		{"firmado con otra llave", firmar(t, otraPrivada, claimsValidos())},
		{"cuerpo cambiado", alterado},
		{"HS256 con un secreto", hs256},
		{"sin exp", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { delete(c, "exp") }))},
		{"otro emisor", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { c["iss"] = "gateway" }))},
		{"otra audiencia", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { c["aud"] = "otro" }))},
		{"sub no es UUID", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { c[ClaimUserID] = "42" }))},
		{"rol desconocido", firmar(t, privadaPrueba, con(func(c jwt.MapClaims) { c[ClaimRol] = "superusuario" }))},
		{"basura", "esto.no.es-un-jwt"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := validador(t).Validar(c.token)
			if !errors.Is(err, ErrTokenInvalido) {
				t.Errorf("error = %v, se esperaba ErrTokenInvalido", err)
			}
		})
	}
}

func TestAlgoritmoNone(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claimsValidos()).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validador(t).Validar(token); !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("un token sin firma no debe pasar: %v", err)
	}
}

func TestDesdeRequest(t *testing.T) {
	v := validador(t)

	t.Run("con identidad valida", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(HeaderIdentidad, firmar(t, privadaPrueba, claimsValidos()))
		if s, err := DesdeRequest(v, r); err != nil || s.UserID != userPrueba {
			t.Errorf("sesion = %+v, err = %v", s, err)
		}
	})

	t.Run("sin identidad", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, err := DesdeRequest(v, r); !errors.Is(err, ErrSinSesion) {
			t.Errorf("error = %v, se esperaba ErrSinSesion", err)
		}
	})

	t.Run("la cookie ya no sirve", func(t *testing.T) {
		// El motor no tiene JWT_SECRET: una cookie de sesion no lo identifica.
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: firmar(t, privadaPrueba, claimsValidos())})
		if _, err := DesdeRequest(v, r); !errors.Is(err, ErrSinSesion) {
			t.Errorf("error = %v, se esperaba ErrSinSesion", err)
		}
	})
}

func TestLlaveInvalida(t *testing.T) {
	if _, err := NuevoValidador(nil); err == nil {
		t.Error("una llave vacia deberia fallar")
	}
}

func TestLeerLlavePublica(t *testing.T) {
	dir := t.TempDir()
	der, err := x509.MarshalPKIXPublicKey(publicaPrueba)
	if err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(dir, "identidad.pub")
	if err := os.WriteFile(ruta, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}

	if llave, err := LeerLlavePublica(ruta); err != nil || !llave.Equal(publicaPrueba) {
		t.Fatalf("LeerLlavePublica = %v, %v", llave, err)
	}
	if _, err := LeerLlavePublica(filepath.Join(dir, "no-existe.pub")); err == nil {
		t.Error("un archivo que no existe deberia fallar")
	}
}
