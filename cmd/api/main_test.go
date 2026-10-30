package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/bookapi/book"
)

type stubStore struct{}

func (stubStore) Get(context.Context, string) (book.Book, error) {
	return book.Book{}, errors.New("stub store")
}

func (stubStore) Put(context.Context, book.Book) error {
	return errors.New("stub store")
}

func (stubStore) Delete(context.Context, string) error {
	return errors.New("stub store")
}

func TestLivenessAndReadiness(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		ready      func(context.Context) error
		wantStatus int
	}{
		{
			name:       "healthz when ready",
			path:       "/healthz",
			ready:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
		},
		{
			name:       "healthz during database outage",
			path:       "/healthz",
			ready:      func(context.Context) error { return errors.New("database down") },
			wantStatus: http.StatusOK,
		},
		{
			name:       "readyz when ready",
			path:       "/readyz",
			ready:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
		},
		{
			name:       "readyz during database outage",
			path:       "/readyz",
			ready:      func(context.Context) error { return errors.New("database down") },
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newMux(tt.ready, stubStore{})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestHealthzNeverConsultsStorage proves liveness is independent of the
// database: the readiness probe panics if it is ever called.
func TestHealthzNeverConsultsStorage(t *testing.T) {
	called := false
	ready := func(context.Context) error {
		called = true
		return errors.New("database down")
	}
	mux := newMux(ready, stubStore{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", rec.Code)
	}
	if called {
		t.Error("healthz consulted storage")
	}
}
