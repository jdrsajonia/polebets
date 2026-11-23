package config

import (
	"strings"
	"testing"
)

func entorno(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func entornoCompleto() map[string]string {
	return map[string]string{
		"USUARIOS_BASE_URL":       "http://usuarios:8001",
		"PAGOS_BASE_URL":          "http://pagos:8000/",
		"MOTOR_BASE_URL":          "http://motor:8080",
		"TOKEN_HACIA_USUARIOS":    "token-de-pruebas-orquestador-a-usuarios",
		"TOKEN_HACIA_PAGOS":       "token-de-pruebas-orquestador-a-pagos-xx",
		"TOKEN_HACIA_MOTOR":       "token-de-pruebas-orquestador-a-motor-xx",
		"IDENTIDAD_LLAVE_PUBLICA": "/run/llaves/identidad.pub",
	}
}

func TestCargaConValoresPorDefecto(t *testing.T) {
	cfg, err := desde(entorno(entornoCompleto()))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if cfg.HTTPPort != "8081" {
		t.Errorf("HTTPPort = %q, se esperaba 8081", cfg.HTTPPort)
	}
	if cfg.PagosBaseURL != "http://pagos:8000" {
		t.Errorf("PagosBaseURL = %q, se esperaba sin la barra final", cfg.PagosBaseURL)
	}
}

func TestHTTPPortOpcional(t *testing.T) {
	vars := entornoCompleto()
	vars["HTTP_PORT"] = "9000"
	cfg, err := desde(entorno(vars))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if cfg.HTTPPort != "9000" {
		t.Errorf("HTTPPort = %q, se esperaba 9000", cfg.HTTPPort)
	}
}

func TestFallaSiFaltaUnaObligatoria(t *testing.T) {
	for _, clave := range []string{"USUARIOS_BASE_URL", "PAGOS_BASE_URL", "MOTOR_BASE_URL", "TOKEN_HACIA_USUARIOS", "TOKEN_HACIA_PAGOS", "TOKEN_HACIA_MOTOR", "IDENTIDAD_LLAVE_PUBLICA"} {
		t.Run(clave, func(t *testing.T) {
			vars := entornoCompleto()
			vars[clave] = "   " // solo espacios cuenta como vacia
			_, err := desde(entorno(vars))
			if err == nil {
				t.Fatalf("se esperaba error sin %s", clave)
			}
			if !strings.Contains(err.Error(), clave) {
				t.Errorf("el error no menciona %s: %v", clave, err)
			}
		})
	}
}

func TestListaTodasLasFaltantes(t *testing.T) {
	_, err := desde(entorno(map[string]string{}))
	if err == nil {
		t.Fatal("se esperaba error con el entorno vacio")
	}
	for _, clave := range []string{"USUARIOS_BASE_URL", "PAGOS_BASE_URL", "MOTOR_BASE_URL", "TOKEN_HACIA_USUARIOS", "TOKEN_HACIA_PAGOS", "TOKEN_HACIA_MOTOR", "IDENTIDAD_LLAVE_PUBLICA"} {
		if !strings.Contains(err.Error(), clave) {
			t.Errorf("el error no menciona %s: %v", clave, err)
		}
	}
}

func TestTokenCorto(t *testing.T) {
	for _, clave := range []string{"TOKEN_HACIA_USUARIOS", "TOKEN_HACIA_PAGOS", "TOKEN_HACIA_MOTOR"} {
		t.Run(clave, func(t *testing.T) {
			vars := entornoCompleto()
			vars[clave] = strings.Repeat("x", LargoMinimoToken-1)
			_, err := desde(entorno(vars))
			if err == nil || !strings.Contains(err.Error(), clave) {
				t.Fatalf("se esperaba error por %s corto, llego %v", clave, err)
			}
		})
	}
}

// Escenario 4: un mismo token para dos destinos es otra vez un token
// compartido, asi que el orquestador no arranca.
func TestTokensRepetidos(t *testing.T) {
	vars := entornoCompleto()
	vars["TOKEN_HACIA_MOTOR"] = vars["TOKEN_HACIA_PAGOS"]
	_, err := desde(entorno(vars))
	if err == nil || !strings.Contains(err.Error(), "mismo valor") {
		t.Fatalf("se esperaba error por tokens repetidos, llego %v", err)
	}
}

func TestCargaUnTokenPorDestino(t *testing.T) {
	vars := entornoCompleto()
	cfg, err := desde(entorno(vars))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if cfg.TokenHaciaUsuarios != vars["TOKEN_HACIA_USUARIOS"] || cfg.TokenHaciaPagos != vars["TOKEN_HACIA_PAGOS"] || cfg.TokenHaciaMotor != vars["TOKEN_HACIA_MOTOR"] {
		t.Errorf("tokens mal asignados: %+v", cfg)
	}
}
