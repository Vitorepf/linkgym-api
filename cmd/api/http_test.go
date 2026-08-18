package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerEmptyIsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	if tok := bearer(req); tok != "" {
		t.Fatalf("got %q", tok)
	}
}

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "telefone_invalido")
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"telefone_invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}
