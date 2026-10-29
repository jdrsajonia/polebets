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
		"USUARIOS_BASE_URL":  "http://usuarios:8001",
		"PAGOS_BASE_URL":     "http://pagos:8000/",
		"MOTOR_BASE_URL":     "http://motor:8080",
		"INTERNAL_API_TOKEN": "token-interno-de-pruebas-orquestador",
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
	for _, clave := range []string{"USUARIOS_BASE_URL", "PAGOS_BASE_URL", "MOTOR_BASE_URL", "INTERNAL_API_TOKEN"} {
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
	for _, clave := range []string{"USUARIOS_BASE_URL", "PAGOS_BASE_URL", "MOTOR_BASE_URL", "INTERNAL_API_TOKEN"} {
		if !strings.Contains(err.Error(), clave) {
			t.Errorf("el error no menciona %s: %v", clave, err)
		}
	}
}

func TestTokenCorto(t *testing.T) {
	vars := entornoCompleto()
	vars["INTERNAL_API_TOKEN"] = strings.Repeat("x", LargoMinimoToken-1)
	if _, err := desde(entorno(vars)); err == nil {
		t.Fatal("se esperaba error con un token de menos de 32 caracteres")
	}
}
