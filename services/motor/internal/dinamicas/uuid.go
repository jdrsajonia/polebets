package dinamicas

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// espacioMercados es el namespace fijo de los market_id dinamicos. No
// cambiarlo: los ids de simulaciones ya jugadas dejarian de coincidir.
var espacioMercados = uuidBytes("8c1f4e2a-6b3d-4f5e-9a7c-2d0b1e3f4a5c")

// uuidV5 implementa el UUID version 5 de la RFC 9562 (SHA-1 del namespace
// mas el nombre). Se hace aqui para no sumar una dependencia al motor.
func uuidV5(ns [16]byte, nombre string) string {
	h := sha1.New()
	h.Write(ns[:])
	h.Write([]byte(nombre))
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // variante RFC
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

func uuidBytes(s string) [16]byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		panic("dinamicas: uuid invalido " + s)
	}
	var u [16]byte
	copy(u[:], b)
	return u
}
