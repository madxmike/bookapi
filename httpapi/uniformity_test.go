package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEveryRouteIsUniform walks the route table itself. A route added to the
// table is exercised here without adding a test: the common headers and, for
// error statuses, the stable envelope are asserted for every entry.
func TestEveryRouteIsUniform(t *testing.T) {
	h := New(newFakeStore(stored), func(context.Context) error { return nil })
	s := h.(*server)

	for _, rt := range s.routes() {
		rt := rt
		t.Run(rt.path(), func(t *testing.T) {
			rec := serveRoute(t, s, rt)
			assertCommonHeaders(t, rec)

			if rec.Code >= 400 && rt.method != http.MethodHead {
				assertEnvelope(t, rec.Body.String())
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("error Content-Type = %q, want application/json", ct)
				}
			}
			if rt.method == http.MethodHead {
				if rec.Body.Len() != 0 {
					t.Errorf("HEAD body = %q, want empty", rec.Body.String())
				}
				if rec.Code >= 400 {
					if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
						t.Errorf("HEAD error Content-Type = %q, want application/json", ct)
					}
					if cl := rec.Header().Get("Content-Length"); cl == "" || cl == "0" {
						t.Errorf("HEAD error Content-Length = %q, want the envelope length", cl)
					}
				}
			}
		})
	}
}

func serveRoute(t *testing.T, h http.Handler, rt route) *httptest.ResponseRecorder {
	t.Helper()
	method := rt.method
	if method == "" {
		method = http.MethodGet
	}
	path := strings.ReplaceAll(rt.pattern, "{isbn}", validISBN13)
	body := ""
	if method == http.MethodPut {
		body = validBody
	}
	return do(t, h, method, path, body)
}

func assertCommonHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func assertEnvelope(t *testing.T, body string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	if len(top) != 1 {
		t.Fatalf("error body has keys %v, want only \"error\": %s", keys(top), body)
	}
	raw, ok := top["error"]
	if !ok {
		t.Fatalf("error body has no \"error\" key: %s", body)
	}
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("decode error detail: %v", err)
	}
	if len(detail) != 2 {
		t.Fatalf("error detail has keys %v, want exactly code and message: %s", keys(detail), body)
	}
	var code, message string
	if err := json.Unmarshal(detail["code"], &code); err != nil {
		t.Fatalf("error code is not a string: %v (%s)", err, body)
	}
	if err := json.Unmarshal(detail["message"], &message); err != nil {
		t.Fatalf("error message is not a string: %v (%s)", err, body)
	}
	if code == "" || message == "" {
		t.Errorf("error envelope has an empty field: %s", body)
	}
}

// TestEveryErrorClassIsUniform covers the error paths that the route table
// walk cannot produce on its own: invalid input, an oversized body, an
// unreachable store, a failing readiness probe, and an unknown route.
func TestEveryErrorClassIsUniform(t *testing.T) {
	ready := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("database down") }
	blocked := blockingStore{}
	leaky := newFakeStore(stored)
	leaky.getErr = errors.New(leakText)

	tests := []struct {
		name       string
		handler    http.Handler
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "healthz", handler: New(newFakeStore(), ready), method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "readyz ready", handler: New(newFakeStore(), ready), method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK},
		{name: "readyz down", handler: New(newFakeStore(), down), method: http.MethodGet, path: "/readyz", wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
		{name: "invalid isbn", handler: New(newFakeStore()), method: http.MethodGet, path: "/books/" + invalidISBN, wantStatus: http.StatusBadRequest, wantCode: "invalid_isbn"},
		{name: "invalid body", handler: New(newFakeStore()), method: http.MethodPut, path: "/books/" + validISBN13, body: `{"title":`, wantStatus: http.StatusBadRequest, wantCode: "invalid_body"},
		{name: "oversized body", handler: New(newFakeStore()), method: http.MethodPut, path: "/books/" + validISBN13, body: `{"title":"` + strings.Repeat("a", 2<<20) + `","author":"a","published_year":1}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "payload_too_large"},
		{name: "unknown route", handler: New(newFakeStore()), method: http.MethodGet, path: "/nope", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "unknown method", handler: New(newFakeStore()), method: http.MethodPost, path: "/books/" + validISBN13, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "store unavailable", handler: New(leaky), method: http.MethodGet, path: "/books/" + validISBN13, wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
		{name: "store timeout", handler: New(blocked), method: http.MethodGet, path: "/books/" + validISBN13, wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
		{name: "delete no content", handler: New(newFakeStore(stored)), method: http.MethodDelete, path: "/books/" + validISBN13, wantStatus: http.StatusNoContent},
		{name: "head missing", handler: New(newFakeStore()), method: http.MethodHead, path: "/books/9780306406157", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, tt.handler, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			assertCommonHeaders(t, rec)

			if tt.wantCode != "" {
				if tt.method == http.MethodHead {
					// HEAD carries the envelope headers but no body.
					if rec.Body.Len() != 0 {
						t.Errorf("HEAD body = %q, want empty", rec.Body.String())
					}
					if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
						t.Errorf("HEAD error Content-Type = %q, want application/json", ct)
					}
					return
				}
				assertEnvelope(t, rec.Body.String())
				assertErrorEnvelope(t, rec.Body.String(), tt.wantCode)
			}
		})
	}
}
