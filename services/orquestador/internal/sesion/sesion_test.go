package sesion

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/arquisoft/orquestador/internal/errores"
)

const idJugador = "0b6c2f1e-8d4a-4c3b-9f2e-1a2b3c4d5e6f"

// parDePrueba genera un par Ed25519 y configura sesion con la publica.
func parDePrueba(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	publica, privada, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	Configurar(publica)
	return privada
}

// claimsValidos son los que pondria Usuarios para un jugador.
func claimsValidos(id, rol string) jwt.MapClaims {
	ahora := time.Now()
	return jwt.MapClaims{
		"sub": id, ClaimRol: rol, "iss": Emisor, "aud": Audiencia,
		"iat": ahora.Unix(), "exp": ahora.Add(time.Minute).Unix(),
	}
}

// firmar crea un token EdDSA con la llave dada.
func firmar(t *testing.T, llave ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(llave)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// con copia los claims validos y les aplica un cambio.
func con(id, rol string, cambiar func(jwt.MapClaims)) jwt.MapClaims {
	c := claimsValidos(id, rol)
	cambiar(c)
	return c
}

// pedir pasa una peticion con las cabeceras dadas por ExigirJugador.
func pedir(cabeceras map[string]string) (*httptest.ResponseRecorder, string, bool) {
	var visto string
	llego := false
	h := ExigirJugador(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llego = true
		visto, _ = UsuarioDe(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/eventos", nil)
	for k, v := range cabeceras {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, visto, llego
}

func TestExigirJugador(t *testing.T) {
	privada := parDePrueba(t)
	_, otraPrivada, _ := ed25519.GenerateKey(rand.Reader)

	bueno := firmar(t, privada, claimsValidos(idJugador, RolJugador))
	partes := strings.Split(bueno, ".")
	// Mismo encabezado y firma, pero el cuerpo dice ser otro jugador.
	otroCuerpo := firmar(t, privada, claimsValidos("11111111-2222-3333-4444-555555555555", RolJugador))
	alterado := partes[0] + "." + strings.Split(otroCuerpo, ".")[1] + "." + partes[2]

	sinFirma, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claimsValidos(idJugador, RolJugador)).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	hs256, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsValidos(idJugador, RolJugador)).
		SignedString([]byte("un-secreto-compartido-cualquiera"))

	casos := []struct {
		nombre     string
		cabeceras  map[string]string
		estado     int
		codigo     string // vacio si debe llegar al handler
		esperadoID string
	}{
		{"sin identidad", nil, 401, "sin_sesion", ""},
		// El ataque del escenario 4: la cabecera escrita a mano ya no sirve.
		{"solo X-User-Id escrito a mano", map[string]string{"X-User-Id": idJugador, "X-User-Rol": RolJugador}, 401, "sin_sesion", ""},
		{"token basura", map[string]string{HeaderIdentidad: "esto.no.es-un-jwt"}, 401, "sin_sesion", ""},
		{"firmado con otra llave", map[string]string{HeaderIdentidad: firmar(t, otraPrivada, claimsValidos(idJugador, RolJugador))}, 401, "sin_sesion", ""},
		{"cuerpo cambiado (otro jugador)", map[string]string{HeaderIdentidad: alterado}, 401, "sin_sesion", ""},
		{"algoritmo none", map[string]string{HeaderIdentidad: sinFirma}, 401, "sin_sesion", ""},
		{"HS256 en vez de EdDSA", map[string]string{HeaderIdentidad: hs256}, 401, "sin_sesion", ""},
		{"vencido", map[string]string{HeaderIdentidad: firmar(t, privada, con(idJugador, RolJugador, func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-time.Second).Unix()
		}))}, 401, "sin_sesion", ""},
		{"sin exp", map[string]string{HeaderIdentidad: firmar(t, privada, con(idJugador, RolJugador, func(c jwt.MapClaims) { delete(c, "exp") }))}, 401, "sin_sesion", ""},
		{"otro emisor", map[string]string{HeaderIdentidad: firmar(t, privada, con(idJugador, RolJugador, func(c jwt.MapClaims) { c["iss"] = "gateway" }))}, 401, "sin_sesion", ""},
		{"otra audiencia", map[string]string{HeaderIdentidad: firmar(t, privada, con(idJugador, RolJugador, func(c jwt.MapClaims) { c["aud"] = "otro-sistema" }))}, 401, "sin_sesion", ""},
		{"sub que no es uuid", map[string]string{HeaderIdentidad: firmar(t, privada, claimsValidos("123", RolJugador))}, 401, "sin_sesion", ""},
		{"rol desconocido", map[string]string{HeaderIdentidad: firmar(t, privada, claimsValidos(idJugador, "jugador"))}, 401, "sin_sesion", ""},
		{"rol en mayusculas", map[string]string{HeaderIdentidad: firmar(t, privada, claimsValidos(idJugador, "PLAYER"))}, 401, "sin_sesion", ""},
		{"admin", map[string]string{HeaderIdentidad: firmar(t, privada, claimsValidos(idJugador, RolAdmin))}, 403, "solo_jugadores", ""},
		{"jugador", map[string]string{HeaderIdentidad: bueno}, 200, "", idJugador},
		{"jugador con uuid en mayusculas", map[string]string{HeaderIdentidad: firmar(t, privada, claimsValidos(strings.ToUpper(idJugador), RolJugador))}, 200, "", idJugador},
		// Si llegan ambas, manda la firma: X-User-Id no cambia la identidad.
		{"X-User-Id distinto al del token", map[string]string{HeaderIdentidad: bueno, "X-User-Id": "11111111-2222-3333-4444-555555555555"}, 200, "", idJugador},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			rec, visto, llego := pedir(c.cabeceras)
			if rec.Code != c.estado {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, c.estado)
			}
			if c.codigo == "" {
				if !llego {
					t.Fatal("la peticion no llego al handler")
				}
				if visto != c.esperadoID {
					t.Errorf("UsuarioDe = %q, se esperaba %q", visto, c.esperadoID)
				}
				return
			}
			if llego {
				t.Fatal("la peticion no debia llegar al handler")
			}
			var cuerpo errores.Cuerpo
			if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
				t.Fatalf("cuerpo no es JSON: %v", err)
			}
			if cuerpo.Codigo != c.codigo || cuerpo.Error == "" {
				t.Errorf("cuerpo = %+v, se esperaba codigo %s", cuerpo, c.codigo)
			}
		})
	}
}

func TestSinLlaveConfiguradaRechazaTodo(t *testing.T) {
	privada := parDePrueba(t)
	token := firmar(t, privada, claimsValidos(idJugador, RolJugador))
	llavePublica.Store(nil)
	defer parDePrueba(t)

	if rec, _, _ := pedir(map[string]string{HeaderIdentidad: token}); rec.Code != 401 {
		t.Errorf("status = %d, se esperaba 401 (falla cerrado)", rec.Code)
	}
}

func TestLeerLlavePublica(t *testing.T) {
	dir := t.TempDir()
	publica, _, _ := ed25519.GenerateKey(rand.Reader)
	der, err := x509.MarshalPKIXPublicKey(publica)
	if err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(dir, "identidad.pub")
	if err := os.WriteFile(ruta, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}

	leida, err := LeerLlavePublica(ruta)
	if err != nil || !leida.Equal(publica) {
		t.Fatalf("LeerLlavePublica = %v, %v", leida, err)
	}
	if _, err := LeerLlavePublica(filepath.Join(dir, "no-existe.pub")); err == nil {
		t.Error("un archivo que no existe deberia fallar")
	}
	basura := filepath.Join(dir, "basura.pub")
	_ = os.WriteFile(basura, []byte("no es una llave"), 0o644)
	if _, err := LeerLlavePublica(basura); err == nil {
		t.Error("un archivo que no es PEM deberia fallar")
	}
}
