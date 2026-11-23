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

// LargoMinimoToken es el largo minimo de cada token de servicio.
const LargoMinimoToken = 32

type Config struct {
	HTTPPort        string
	UsuariosBaseURL string
	PagosBaseURL    string
	MotorBaseURL    string

	// Tokens de servicio (escenario 4, T3-05): uno por cada destino. Cada
	// servicio acepta solo el token que le corresponde, asi que el de Pagos
	// no sirve contra Usuarios ni contra el Motor.
	TokenHaciaUsuarios string
	TokenHaciaPagos    string
	TokenHaciaMotor    string

	// IdentidadLlavePublica es la ruta de la llave publica Ed25519 de Usuarios,
	// con la que se verifica X-User-Token (escenario 4, T3-05).
	IdentidadLlavePublica string
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
		UsuariosBaseURL: sinBarraFinal(requerida("USUARIOS_BASE_URL")),
		PagosBaseURL:    sinBarraFinal(requerida("PAGOS_BASE_URL")),
		MotorBaseURL:    sinBarraFinal(requerida("MOTOR_BASE_URL")),
		HTTPPort:        opcional(getenv, "HTTP_PORT", "8081"),

		TokenHaciaUsuarios: requerida("TOKEN_HACIA_USUARIOS"),
		TokenHaciaPagos:    requerida("TOKEN_HACIA_PAGOS"),
		TokenHaciaMotor:    requerida("TOKEN_HACIA_MOTOR"),

		IdentidadLlavePublica: requerida("IDENTIDAD_LLAVE_PUBLICA"),
	}

	if len(faltan) > 0 {
		return Config{}, fmt.Errorf("faltan variables de entorno obligatorias: %s", strings.Join(faltan, ", "))
	}
	if err := validarTokens([][2]string{
		{"TOKEN_HACIA_USUARIOS", cfg.TokenHaciaUsuarios},
		{"TOKEN_HACIA_PAGOS", cfg.TokenHaciaPagos},
		{"TOKEN_HACIA_MOTOR", cfg.TokenHaciaMotor},
	}); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validarTokens exige que cada token (nombre, valor) tenga el largo minimo y
// que no se repitan: si alguien copia el mismo valor en los tres, se vuelve
// al token compartido que el escenario 4 quiere eliminar.
func validarTokens(tokens [][2]string) error {
	vistos := map[string]string{}
	for _, t := range tokens {
		nombre, valor := t[0], t[1]
		if len(valor) < LargoMinimoToken {
			return fmt.Errorf("%s debe tener al menos %d caracteres", nombre, LargoMinimoToken)
		}
		if otro, ok := vistos[valor]; ok {
			return fmt.Errorf("%s y %s tienen el mismo valor: cada destino necesita su propio token", otro, nombre)
		}
		vistos[valor] = nombre
	}
	return nil
}

// opcional lee una variable y usa el valor por defecto si esta vacia.
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
