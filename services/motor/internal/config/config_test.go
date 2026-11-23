package config

import (
	"strings"
	"testing"
	"time"
)

func entorno(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func entornoCompleto() map[string]string {
	return map[string]string{
		"MONGO_URI":               "mongodb://localhost:27017",
		"MONGO_DB":                "apuestas_db",
		"IDENTIDAD_LLAVE_PUBLICA": "/run/llaves/identidad.pub",
		"PAGOS_BASE_URL":          "http://pagos:8000",
		"TOKEN_DESDE_ORQUESTADOR": "token-de-pruebas-orquestador-a-motor-xx",
		"TOKEN_HACIA_PAGOS":       "token-de-pruebas-motor-a-pagos-xxxxxxxx",
		"ALLOWED_ORIGINS":         "http://localhost:5173, http://127.0.0.1:5173",
	}
}

func TestCargaConValoresPorDefecto(t *testing.T) {
	cfg, err := desde(entorno(entornoCompleto()))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, se esperaba 8080", cfg.HTTPPort)
	}
	if cfg.VentanaApuestas != 30*time.Second || cfg.VentanaObservacion != 60*time.Second {
		t.Errorf("ventanas = %v / %v, se esperaba 30s / 60s", cfg.VentanaApuestas, cfg.VentanaObservacion)
	}
	if len(cfg.OrigenesPermitidos) != 2 || cfg.OrigenesPermitidos[1] != "http://127.0.0.1:5173" {
		t.Errorf("origenes = %v", cfg.OrigenesPermitidos)
	}
}

func TestFallaSiFaltanObligatorias(t *testing.T) {
	vars := entornoCompleto()
	delete(vars, "IDENTIDAD_LLAVE_PUBLICA")
	delete(vars, "MONGO_URI")
	delete(vars, "TOKEN_DESDE_ORQUESTADOR")
	delete(vars, "TOKEN_HACIA_PAGOS")

	_, err := desde(entorno(vars))
	if err == nil {
		t.Fatal("se esperaba error por variables faltantes")
	}
	for _, clave := range []string{"IDENTIDAD_LLAVE_PUBLICA", "MONGO_URI", "TOKEN_DESDE_ORQUESTADOR", "TOKEN_HACIA_PAGOS"} {
		if !strings.Contains(err.Error(), clave) {
			t.Errorf("el error no menciona %s: %v", clave, err)
		}
	}
}

func TestOrigenesVacios(t *testing.T) {
	vars := entornoCompleto()
	vars["ALLOWED_ORIGINS"] = " , ,"
	if _, err := desde(entorno(vars)); err == nil {
		t.Fatal("se esperaba error con ALLOWED_ORIGINS sin origenes")
	}
}

func TestVentanaInvalida(t *testing.T) {
	for _, v := range []string{"abc", "0", "-5"} {
		vars := entornoCompleto()
		vars["BETTING_WINDOW_SECONDS"] = v
		if _, err := desde(entorno(vars)); err == nil {
			t.Errorf("BETTING_WINDOW_SECONDS=%q deberia fallar", v)
		}
	}
}

func TestTokenInternoCorto(t *testing.T) {
	for _, clave := range []string{"TOKEN_DESDE_ORQUESTADOR", "TOKEN_HACIA_PAGOS"} {
		vars := entornoCompleto()
		vars[clave] = "demasiado-corto"
		_, err := desde(entorno(vars))
		if err == nil || !strings.Contains(err.Error(), clave) {
			t.Errorf("se esperaba rechazo de %s corto, llego %v", clave, err)
		}
	}
}

// Escenario 4: con el mismo valor, Pagos podria llamar al motor con el token
// que el motor le manda. El motor no arranca.
func TestTokensRepetidos(t *testing.T) {
	vars := entornoCompleto()
	vars["TOKEN_HACIA_PAGOS"] = vars["TOKEN_DESDE_ORQUESTADOR"]
	_, err := desde(entorno(vars))
	if err == nil || !strings.Contains(err.Error(), "mismo valor") {
		t.Fatalf("se esperaba error por tokens repetidos, llego %v", err)
	}
}
