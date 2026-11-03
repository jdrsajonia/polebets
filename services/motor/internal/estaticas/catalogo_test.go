package estaticas

import "testing"

func TestMultiplicadorPorTramo(t *testing.T) {
	casos := map[string]float64{
		"VER": 3.0,  // top
		"PIA": 3.0,  // top
		"HAM": 6.0,  // alto
		"GAS": 10.0, // medio
		"TSU": 15.0, // fuera de la tabla -> por defecto
		"XYZ": 15.0, // piloto desconocido -> por defecto
	}
	for codigo, esperado := range casos {
		if got := multiplicadorDe(codigo); got != esperado {
			t.Errorf("multiplicadorDe(%q) = %v, se esperaba %v", codigo, got, esperado)
		}
	}
}

func TestOpcionesGanadorOrdenadasYConMultiplicador(t *testing.T) {
	pilotos := map[int]string{1: "VER", 44: "HAM", 10: "GAS"}
	ops := opcionesGanador(pilotos)

	if len(ops) != 3 {
		t.Fatalf("len = %d, se esperaban 3", len(ops))
	}
	// Orden alfabetico por codigo: GAS, HAM, VER (determinista, RNF-20).
	if ops[0].Nombre != "GAS" || ops[1].Nombre != "HAM" || ops[2].Nombre != "VER" {
		t.Errorf("orden = %v, se esperaba [GAS HAM VER]", []string{ops[0].Nombre, ops[1].Nombre, ops[2].Nombre})
	}
	if ops[0].Multiplicador != 10.0 || ops[1].Multiplicador != 6.0 || ops[2].Multiplicador != 3.0 {
		t.Errorf("multiplicadores = %v, se esperaba [10 6 3]", []float64{ops[0].Multiplicador, ops[1].Multiplicador, ops[2].Multiplicador})
	}
}

func TestOpcionesGanadorOmitePilotosSinCodigoYRepetidos(t *testing.T) {
	pilotos := map[int]string{1: "VER", 2: "", 3: "VER", 4: "NOR"}
	ops := opcionesGanador(pilotos)

	if len(ops) != 2 {
		t.Fatalf("len = %d, se esperaban 2 (NOR, VER sin repetir ni vacio)", len(ops))
	}
	if ops[0].Nombre != "NOR" || ops[1].Nombre != "VER" {
		t.Errorf("codigos = %v, se esperaba [NOR VER]", []string{ops[0].Nombre, ops[1].Nombre})
	}
}

func TestIDMercadoEsDeterministaYDistintoPorEvento(t *testing.T) {
	a := IDMercado("evt-1", PlantillaGanador)
	b := IDMercado("evt-1", PlantillaGanador)
	c := IDMercado("evt-2", PlantillaGanador)
	if a != b {
		t.Errorf("mismo evento/plantilla dio ids distintos: %s vs %s", a, b)
	}
	if a == c {
		t.Errorf("eventos distintos dieron el mismo id: %s", a)
	}
	if len(a) != 36 {
		t.Errorf("id no parece un uuid: %q", a)
	}
}
