package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"example.com/bookapi/book"
)

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestList(t *testing.T) {
	first := book.Book{ISBN: "9780000000019", Title: "A", Author: "Alice", PublishedYear: 2001}
	second := book.Book{ISBN: "9780000000026", Title: "B", Author: "Bob", PublishedYear: 2002}
	third := book.Book{ISBN: "9780000000033", Title: "C", Author: "Alice", PublishedYear: 2003}

	t.Run("ordered by isbn", func(t *testing.T) {
		// Insert out of order; the response must still be sorted.
		h := New(newFakeStore(third, first, second))
		rec := do(t, h, http.MethodGet, "/books", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got []bookResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		want := []bookResponse{toResponse(first), toResponse(second), toResponse(third)}
		if len(got) != len(want) {
			t.Fatalf("books = %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("book %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("author filter is exact", func(t *testing.T) {
		h := New(newFakeStore(first, second, third))
		rec := do(t, h, http.MethodGet, "/books?author=Alice", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got []bookResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		want := []bookResponse{toResponse(first), toResponse(third)}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("books = %+v, want %+v", got, want)
		}
	})

	t.Run("no matches is an empty array", func(t *testing.T) {
		h := New(newFakeStore(first, second))
		rec := do(t, h, http.MethodGet, "/books?author=Nobody", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); body != "[]" {
			t.Errorf("body = %q, want []", body)
		}
	})

	t.Run("store error is 503 envelope", func(t *testing.T) {
		st := newFakeStore(first)
		st.listErr = errors.New(leakText)
		h := New(st)
		rec := do(t, h, http.MethodGet, "/books", "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (body %q)", rec.Code, rec.Body.String())
		}
		assertErrorEnvelope(t, rec.Body.String(), "unavailable")
	})
}

func TestListTimeoutReturns503(t *testing.T) {
	h := New(blockingStore{})
	rec := do(t, h, http.MethodGet, "/books", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %q)", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.String(), "unavailable")
}

// TestHeadMirrorsGet checks the HEAD contract at the response level: the same
// status and headers as GET, including Content-Length, and no body.
func TestHeadMirrorsGet(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		store *fakeStore
	}{
		{name: "stored", path: "/books/" + validISBN13, store: newFakeStore(stored)},
		{name: "missing", path: "/books/9780306406157", store: newFakeStore()},
		{name: "invalid isbn", path: "/books/" + invalidISBN, store: newFakeStore()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.store)
			get := do(t, h, http.MethodGet, tt.path, "")
			head := do(t, h, http.MethodHead, tt.path, "")

			if head.Code != get.Code {
				t.Errorf("HEAD status = %d, GET status = %d", head.Code, get.Code)
			}
			for _, key := range []string{"Content-Type", "Content-Length", "X-Content-Type-Options", "Cache-Control"} {
				if got, want := head.Header().Get(key), get.Header().Get(key); got != want {
					t.Errorf("HEAD %s = %q, GET %s = %q", key, got, key, want)
				}
			}
			if got, want := head.Header().Get("Content-Length"), len(get.Body.Bytes()); got != strconv.Itoa(want) {
				t.Errorf("HEAD Content-Length = %q, want %d", got, want)
			}
			if head.Body.Len() != 0 {
				t.Errorf("HEAD body = %q, want empty", head.Body.String())
			}
			if get.Body.Len() == 0 {
				t.Error("GET body is empty; HEAD cannot be shown to mirror it")
			}
		})
	}
}

func TestListEmptyAuthorIsAnExactFilter(t *testing.T) {
	// An empty author parameter is an exact-match filter, not "no filter".
	h := New(newFakeStore(stored))
	rec := do(t, h, http.MethodGet, "/books?author=", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}
