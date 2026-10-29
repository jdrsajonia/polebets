// Package config lee la configuracion del orquestador desde variables de
// entorno (seccion 4 de documentation/orquestador-api.md).
//
// Las variables obligatorias no tienen valor por defecto: si falta alguna,
// el orquestador no arranca. Asi nunca corre con un token de ejemplo escrito
// en el codigo.
package config

import (
	"fmt"
	"os"
	"strings"
)

// LargoMinimoToken es el largo minimo de INTERNAL_API_TOKEN.
const LargoMinimoToken = 32

type Config struct {
	HTTPPort         string
	UsuariosBaseURL  string
	PagosBaseURL     string
	MotorBaseURL     string
	InternalAPIToken string
}

// Cargar lee la configuracion del entorno del proceso.
func Cargar() (Config, error) {
	return desde(os.Getenv)
}

// desde recibe la funcion de lectura para poder probarla sin tocar el
// entorno real.
func desde(getenv func(string) string) (Config, error) {
	var faltan []string
	requerida := func(clave string) string {
		v := strings.TrimSpace(getenv(clave))
		if v == "" {
			faltan = append(faltan, clave)
		}
		return v
	}

	cfg := Config{
		UsuariosBaseURL:  sinBarraFinal(requerida("USUARIOS_BASE_URL")),
		PagosBaseURL:     sinBarraFinal(requerida("PAGOS_BASE_URL")),
		MotorBaseURL:     sinBarraFinal(requerida("MOTOR_BASE_URL")),
		InternalAPIToken: requerida("INTERNAL_API_TOKEN"),
		HTTPPort:         opcional(getenv, "HTTP_PORT", "8081"),
	}

	if len(faltan) > 0 {
		return Config{}, fmt.Errorf("faltan variables de entorno obligatorias: %s", strings.Join(faltan, ", "))
	}
	if len(cfg.InternalAPIToken) < LargoMinimoToken {
		return Config{}, fmt.Errorf("INTERNAL_API_TOKEN debe tener al menos %d caracteres", LargoMinimoToken)
	}
	return cfg, nil
}

func opcional(getenv func(string) string, clave, def string) string {
	if v := strings.TrimSpace(getenv(clave)); v != "" {
		return v
	}
	return def
}

// sinBarraFinal deja "http://pagos:8000/" como "http://pagos:8000", para
// armar las rutas con BaseURL + "/internal/...".
func sinBarraFinal(url string) string {
	return strings.TrimRight(url, "/")
}
