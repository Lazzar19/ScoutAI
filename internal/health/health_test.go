package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHealthDoesNotNeedDatabase(t *testing.T) {
	handler := Handler(pingFunc(func(context.Context) error {
		t.Fatal("liveness pinged a dependency")
		return nil
	}), slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertStatus(t, rec.Body, "ok")
}

func TestReadyWhenDatabaseAnswers(t *testing.T) {
	handler := Handler(pingFunc(func(context.Context) error { return nil }), slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertStatus(t, rec.Body, "ready")
}

func TestReadyWhenDatabaseIsDown(t *testing.T) {
	handler := Handler(pingFunc(func(context.Context) error {
		return errors.New("connection refused")
	}), slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	assertStatus(t, rec.Body, "unavailable")
}

func assertStatus(t *testing.T, body io.Reader, want string) {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Status != want {
		t.Fatalf("status field = %q, want %q", payload.Status, want)
	}
}
