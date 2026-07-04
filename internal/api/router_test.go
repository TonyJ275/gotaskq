package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockPinger implements the Pinger interface for testing
type mockPinger struct {
	err error
}

// Compile-time assertion: Ensures mockPinger strictly implements api.Pinger
var _ Pinger = (*mockPinger)(nil)

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

func TestHealthCheck_Success(t *testing.T) {
	pinger := &mockPinger{err: nil}
	handler, _ := newTestHandler()
	router := NewRouter(handler, pinger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rr.Code)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	pinger := &mockPinger{err: errors.New("database connection refused")}
	handler, _ := newTestHandler()
	router := NewRouter(handler, pinger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable, got %d", rr.Code)
	}
}

func TestRouter_Wiring(t *testing.T) {
	pinger := &mockPinger{err: nil}
	handler, _ := newTestHandler()
	router := NewRouter(handler, pinger)

	// Verify routes are registered by checking for a 400 Bad Request
	// (due to empty body) rather than a 404 Not Found.
	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Errorf("expected route to be registered, got 404 Not Found")
	}
}
