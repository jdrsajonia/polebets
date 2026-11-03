package estaticas

import "testing"

var pilotosPrueba = map[int]string{1: "VER", 44: "HAM", 4: "NOR"}

func TestGanadorEsElUltimoPrimero(t *testing.T) {
	// VER iba 1o al principio, pero NOR termina 1o (evento mas tardio).
	pos := []EventoPosicion{
		{T: 1000, Piloto: 1, Pos: 1},
		{T: 1000, Piloto: 4, Pos: 2},
		{T: 5000, Piloto: 4, Pos: 1},
		{T: 5000, Piloto: 1, Pos: 2},
	}
	g, ok := ganador(pos, pilotosPrueba)
	if !ok || g != "NOR" {
		t.Fatalf("ganador = (%q, %v), se esperaba (NOR, true)", g, ok)
	}
}

func TestGanadorConUnSoloPiloto(t *testing.T) {
	pos := []EventoPosicion{{T: 3000, Piloto: 44, Pos: 1}}
	g, ok := ganador(pos, pilotosPrueba)
	if !ok || g != "HAM" {
		t.Fatalf("ganador = (%q, %v), se esperaba (HAM, true)", g, ok)
	}
}

func TestSinEventosNoHayGanador(t *testing.T) {
	if g, ok := ganador(nil, pilotosPrueba); ok {
		t.Errorf("ganador = %q, se esperaba sin ganador", g)
	}
}

func TestLiderSinCodigoConocidoNoResuelve(t *testing.T) {
	// El driver_number 99 no esta en el mapa de pilotos.
	pos := []EventoPosicion{{T: 2000, Piloto: 99, Pos: 1}}
	if g, ok := ganador(pos, pilotosPrueba); ok {
		t.Errorf("ganador = %q, se esperaba sin ganador (lider sin codigo)", g)
	}
}

func TestPosicionesCeroONegativasSeIgnoran(t *testing.T) {
	// Un dato con pos 0 (dato sucio) no debe tumbar la resolucion.
	pos := []EventoPosicion{
		{T: 1000, Piloto: 1, Pos: 0},
		{T: 2000, Piloto: 1, Pos: 1},
	}
	g, ok := ganador(pos, pilotosPrueba)
	if !ok || g != "VER" {
		t.Fatalf("ganador = (%q, %v), se esperaba (VER, true)", g, ok)
	}
}
