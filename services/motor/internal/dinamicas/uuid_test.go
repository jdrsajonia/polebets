package dinamicas

import "testing"

func TestIDMercadoUUIDv5(t *testing.T) {
	// Vector conocido de la RFC: namespace DNS + "www.example.com".
	dns := uuidBytes("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	if got := uuidV5(dns, "www.example.com"); got != "2ed6657d-e927-568b-95e1-2665a8aea6a2" {
		t.Fatalf("uuidV5 = %s", got)
	}
	a := IDMercado("sim-1", PlantillaSafetyCar, 120000)
	if a != IDMercado("sim-1", PlantillaSafetyCar, 120000) {
		t.Error("el id debe ser determinista")
	}
	for _, otro := range []string{
		IDMercado("sim-2", PlantillaSafetyCar, 120000),
		IDMercado("sim-1", PlantillaCambioLider, 120000),
		IDMercado("sim-1", PlantillaSafetyCar, 120001),
	} {
		if otro == a {
			t.Errorf("ids que deberian diferir: %s", a)
		}
	}
}
