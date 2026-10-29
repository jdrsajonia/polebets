package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthResponde200SinSesion(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var cuerpo map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("cuerpo no es JSON: %v", err)
	}
	if cuerpo["estado"] != "ok" {
		t.Errorf("cuerpo = %v, se esperaba estado ok", cuerpo)
	}
}

func TestHealthSoloAceptaGET(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, se esperaba 405", rec.Code)
	}
}
