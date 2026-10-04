package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogRequestsRecordsStatus(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := logRequests(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	out := buf.String()
	if !strings.Contains(out, "status=418") || !strings.Contains(out, "path=/x") || !strings.Contains(out, "method=GET") {
		t.Fatalf("log = %q", out)
	}
}

func TestLogRequestsRecoversPanics(t *testing.T) {
	var buf bytes.Buffer
	h := logRequests(slog.New(slog.NewTextHandler(&buf, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("panic not logged: %q", buf.String())
	}
}
