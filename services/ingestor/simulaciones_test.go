package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// almacenFalso guarda las simulaciones en memoria.
type almacenFalso struct {
	carreras map[string]bool
	sims     []bson.M
	falla    error
}

func (a *almacenFalso) carreraDescargada(_ context.Context, id string) (bool, error) {
	return a.carreras[id], a.falla
}

func (a *almacenFalso) haySimulacionActiva(context.Context) (bool, error) {
	for _, s := range a.sims {
		if s["estado"] == estadoProgramada || s["estado"] == estadoEnCurso {
			return true, nil
		}
	}
	return false, a.falla
}

func (a *almacenFalso) crear(_ context.Context, doc bson.M) error {
	a.sims = append(a.sims, doc)
	return a.falla
}

var ahoraPrueba = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)

func TestProgramarUsaLosMinutosDeLaOrden(t *testing.T) {
	alm := &almacenFalso{carreras: map[string]bool{"9165": true}}
	r := programarSimulacion(context.Background(), alm, ordenSimulacion{CarreraID: "9165", Minutos: 3}, ahoraPrueba)
	if !r.OK || r.SimulacionID == "" || r.HoraLargada == nil {
		t.Fatalf("respuesta = %+v", r)
	}
	if want := ahoraPrueba.Add(3 * time.Minute); !r.HoraLargada.Equal(want) {
		t.Errorf("hora_largada = %v, want %v", r.HoraLargada, want)
	}
	doc := alm.sims[0]
	if doc["estado"] != estadoProgramada || doc["carrera_id"] != "9165" || doc["t_inicio_ms"] != int64(0) || doc["_id"] != r.SimulacionID {
		t.Errorf("documento = %v", doc)
	}
}

func TestSegundaSimulacionSeRechaza(t *testing.T) {
	alm := &almacenFalso{carreras: map[string]bool{"9165": true, "9158": true}}
	if r := programarSimulacion(context.Background(), alm, ordenSimulacion{CarreraID: "9165", Minutos: 1}, ahoraPrueba); !r.OK {
		t.Fatalf("la primera debería programarse: %+v", r)
	}
	for _, estado := range []string{estadoProgramada, estadoEnCurso} {
		alm.sims[0]["estado"] = estado
		r := programarSimulacion(context.Background(), alm, ordenSimulacion{CarreraID: "9158", Minutos: 1}, ahoraPrueba)
		if r.OK || r.Error != errYaHaySimulacion {
			t.Errorf("con una %s: %+v, want %s", estado, r, errYaHaySimulacion)
		}
	}
	if len(alm.sims) != 1 {
		t.Errorf("se guardaron %d simulaciones, want 1", len(alm.sims))
	}
	// Terminada la anterior, se puede programar otra.
	alm.sims[0]["estado"] = estadoFinalizada
	if r := programarSimulacion(context.Background(), alm, ordenSimulacion{CarreraID: "9158", Minutos: 1}, ahoraPrueba); !r.OK {
		t.Errorf("tras finalizar debería aceptarse: %+v", r)
	}
}

func TestProgramarRechazaOrdenesInvalidas(t *testing.T) {
	alm := &almacenFalso{carreras: map[string]bool{"9165": true}}
	casos := map[string]struct {
		orden ordenSimulacion
		err   string
	}{
		"carrera no descargada": {ordenSimulacion{CarreraID: "1234", Minutos: 1}, errCarreraNoDescargada},
		"sin carrera":           {ordenSimulacion{Minutos: 1}, errOrdenInvalida},
		"0 minutos":             {ordenSimulacion{CarreraID: "9165", Minutos: 0}, errOrdenInvalida},
		"más de 120 minutos":    {ordenSimulacion{CarreraID: "9165", Minutos: 121}, errOrdenInvalida},
		"inicio negativo":       {ordenSimulacion{CarreraID: "9165", Minutos: 1, TInicioMs: -1}, errOrdenInvalida},
	}
	for nombre, c := range casos {
		if r := programarSimulacion(context.Background(), alm, c.orden, ahoraPrueba); r.OK || r.Error != c.err {
			t.Errorf("%s: %+v, want %s", nombre, r, c.err)
		}
	}
	if len(alm.sims) != 0 {
		t.Errorf("no debió guardarse nada: %v", alm.sims)
	}
}

func TestProgramarConMongoCaidoEsErrorInterno(t *testing.T) {
	alm := &almacenFalso{carreras: map[string]bool{"9165": true}, falla: errors.New("sin conexión")}
	if r := programarSimulacion(context.Background(), alm, ordenSimulacion{CarreraID: "9165", Minutos: 1}, ahoraPrueba); r.OK || r.Error != errInterno {
		t.Errorf("%+v, want %s", r, errInterno)
	}
}
