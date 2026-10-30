package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/bookapi/book"
	"example.com/bookapi/store"
)

const (
	validISBN13 = "9780131103627"
	validISBN10 = "0131103628"
	invalidISBN = "978013110362" // bad check digit
	validBody   = `{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}`
	leakText    = `pq: SQLSTATE 08006 connection refused /etc/postgresql/data`
)

var stored = book.Book{
	ISBN:          validISBN13,
	Title:         "The C Programming Language",
	Author:        "Kernighan and Ritchie",
	PublishedYear: 1988,
}

type fakeStore struct {
	books          map[string]book.Book
	getErr         error
	putErr         error
	delErr         error
	lastPut        book.Book
	puts           int
	deletes        int
	lastDeleteISBN string
}

func newFakeStore(bs ...book.Book) *fakeStore {
	f := &fakeStore{books: make(map[string]book.Book, len(bs))}
	for _, b := range bs {
		f.books[b.ISBN] = b
	}
	return f
}

func (f *fakeStore) Get(_ context.Context, isbn string) (book.Book, error) {
	if f.getErr != nil {
		return book.Book{}, f.getErr
	}
	b, ok := f.books[isbn]
	if !ok {
		return book.Book{}, store.ErrNotFound
	}
	return b, nil
}

func (f *fakeStore) Put(_ context.Context, b book.Book) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.puts++
	f.lastPut = b
	f.books[b.ISBN] = b
	return nil
}

func (f *fakeStore) Delete(_ context.Context, isbn string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deletes++
	f.lastDeleteISBN = isbn
	delete(f.books, isbn)
	return nil
}

// missingReportingStore models a store that reports absence from Delete
// instead of treating it as success. The HTTP contract still requires 204.
type missingReportingStore struct{ *fakeStore }

func (s missingReportingStore) Delete(ctx context.Context, isbn string) error {
	if _, ok := s.books[isbn]; !ok {
		return store.ErrNotFound
	}
	return s.fakeStore.Delete(ctx, isbn)
}

func TestHandlers(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		store      *fakeStore
		wantStatus int
		wantCode   string
		wantBook   *book.Book
	}{
		{
			name:       "get stored book",
			method:     http.MethodGet,
			path:       "/books/" + validISBN13,
			store:      newFakeStore(stored),
			wantStatus: http.StatusOK,
			wantBook:   &stored,
		},
		{
			name:       "get stored book via isbn-10",
			method:     http.MethodGet,
			path:       "/books/" + validISBN10,
			store:      newFakeStore(stored),
			wantStatus: http.StatusOK,
			wantBook:   &stored,
		},
		{
			name:       "get missing book",
			method:     http.MethodGet,
			path:       "/books/9780306406157",
			store:      newFakeStore(),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "get bad checksum",
			method:     http.MethodGet,
			path:       "/books/" + invalidISBN,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_isbn",
		},
		{
			name:       "get 977 prefix with valid checksum",
			method:     http.MethodGet,
			path:       "/books/9770131103628",
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_isbn",
		},
		{
			name:       "put creates book",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       validBody,
			store:      newFakeStore(),
			wantStatus: http.StatusOK,
			wantBook:   &stored,
		},
		{
			name:       "put invalid isbn",
			method:     http.MethodPut,
			path:       "/books/" + invalidISBN,
			body:       validBody,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_isbn",
		},
		{
			name:       "put malformed body",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       `{"title":`,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_body",
		},
		{
			name:       "put unknown field",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       `{"title":"t","author":"a","published_year":1,"isbn":"` + validISBN13 + `"}`,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_body",
		},
		{
			name:       "put empty title",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       `{"title":"","author":"a","published_year":1}`,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_body",
		},
		{
			name:       "put empty author",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       `{"title":"t","author":"","published_year":1}`,
			store:      newFakeStore(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_body",
		},
		{
			name:       "put oversized body",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       `{"title":"` + strings.Repeat("a", 2<<20) + `","author":"a","published_year":1}`,
			store:      newFakeStore(),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "payload_too_large",
		},
		{
			name:       "put oversized trailing body",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       validBody + strings.Repeat(" ", 2<<20),
			store:      newFakeStore(),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "payload_too_large",
		},
		{
			name:       "get store error",
			method:     http.MethodGet,
			path:       "/books/" + validISBN13,
			store:      &fakeStore{books: map[string]book.Book{}, getErr: errors.New(leakText)},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "unavailable",
		},
		{
			name:       "put store error",
			method:     http.MethodPut,
			path:       "/books/" + validISBN13,
			body:       validBody,
			store:      &fakeStore{books: map[string]book.Book{}, putErr: errors.New(leakText)},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "unavailable",
		},
		{
			name:       "unknown route",
			method:     http.MethodGet,
			path:       "/nope",
			store:      newFakeStore(),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "unknown method on book route",
			method:     http.MethodPost,
			path:       "/books/" + validISBN13,
			store:      newFakeStore(),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := tt.store
			if st == nil {
				st = newFakeStore()
			}
			h := New(st)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			body := rec.Body.String()

			if tt.wantCode != "" {
				assertErrorEnvelope(t, body, tt.wantCode)
			}

			if tt.wantBook != nil {
				var got bookResponse
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				want := toResponse(*tt.wantBook)
				if got != want {
					t.Errorf("body = %+v, want %+v", got, want)
				}
			}

			if tt.wantStatus != http.StatusOK && st.puts != 0 {
				t.Errorf("store received %d writes on a failed request", st.puts)
			}
		})
	}
}

var allowedErrorMessages = map[string]map[string]bool{
	"invalid_isbn": {"isbn is not valid": true},
	"invalid_body": {
		"request body must be a JSON object with title, author, and published_year": true,
		"title and author must not be empty":                                        true,
	},
	"payload_too_large": {"request body exceeds 1 MiB": true},
	"not_found": {
		"no book with that isbn": true,
		"no such route":          true,
	},
	"unavailable": {"the book store is unavailable": true},
}

// assertErrorEnvelope requires the exact contract shape: one "error" object
// with exactly code and message, both strings, and a message from the fixed
// set for that code. Any extra field, driver text, stack trace, or file path
// fails, even if it does not match one of a few known substrings.
func assertErrorEnvelope(t *testing.T, body, wantCode string) {
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
	if code != wantCode {
		t.Errorf("error code = %q, want %q", code, wantCode)
	}
	allowed, ok := allowedErrorMessages[code]
	if !ok {
		t.Fatalf("error code %q has no declared stable message", code)
	}
	if !allowed[message] {
		t.Errorf("error message = %q, want one of the stable messages for %q: %v", message, code, keys(allowed))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestStoreTimeoutReturns503(t *testing.T) {
	h := New(blockingStore{})

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/books/" + validISBN13},
		{method: http.MethodPut, path: "/books/" + validISBN13, body: validBody},
		{method: http.MethodDelete, path: "/books/" + validISBN13},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			start := time.Now()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			elapsed := time.Since(start)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			if elapsed > 2*time.Second {
				t.Errorf("request took %v, want at most 2s", elapsed)
			}
			assertErrorEnvelope(t, rec.Body.String(), "unavailable")
		})
	}
}

// blockingStore simulates an unreachable database: every call waits for the
// handler's context deadline instead of returning an error immediately.
type blockingStore struct{}

func (blockingStore) Get(ctx context.Context, _ string) (book.Book, error) {
	<-ctx.Done()
	return book.Book{}, ctx.Err()
}

func (blockingStore) Put(ctx context.Context, _ book.Book) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingStore) Delete(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestDelete(t *testing.T) {
	serve := func(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("existing book returns 204 with empty body", func(t *testing.T) {
		st := newFakeStore(stored)

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN13, "")

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "" {
			t.Errorf("Content-Type = %q, want none", ct)
		}
		if _, ok := st.books[validISBN13]; ok {
			t.Errorf("book %q still in store after delete", validISBN13)
		}
	})

	t.Run("repeat delete returns 204", func(t *testing.T) {
		st := newFakeStore(stored)
		h := New(st)

		for i := 0; i < 2; i++ {
			rec := serve(t, h, http.MethodDelete, "/books/"+validISBN13, "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete %d: status = %d, want 204 (body %q)", i+1, rec.Code, rec.Body.String())
			}
			if rec.Body.Len() != 0 {
				t.Errorf("delete %d: body = %q, want empty", i+1, rec.Body.String())
			}
		}
	})

	t.Run("absent book returns 204", func(t *testing.T) {
		st := newFakeStore()

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN13, "")

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "" {
			t.Errorf("Content-Type = %q, want none", ct)
		}
	})

	t.Run("store reporting absence still returns 204", func(t *testing.T) {
		st := missingReportingStore{newFakeStore()}

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN13, "")

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", rec.Body.String())
		}
	})

	t.Run("get after delete returns 404", func(t *testing.T) {
		st := newFakeStore(stored)
		h := New(st)

		if rec := serve(t, h, http.MethodDelete, "/books/"+validISBN13, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("delete: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}

		rec := serve(t, h, http.MethodGet, "/books/"+validISBN13, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("get: status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
		}
		assertErrorEnvelope(t, rec.Body.String(), "not_found")
	})

	t.Run("invalid checksum returns 400 without calling store", func(t *testing.T) {
		st := newFakeStore(stored)

		rec := serve(t, New(st), http.MethodDelete, "/books/"+invalidISBN, "")

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		assertErrorEnvelope(t, rec.Body.String(), "invalid_isbn")
		if st.deletes != 0 {
			t.Errorf("store received %d deletes, want 0", st.deletes)
		}
		if _, ok := st.books[validISBN13]; !ok {
			t.Errorf("invalid delete changed storage")
		}
	})

	t.Run("store error returns 503 without leaking detail", func(t *testing.T) {
		st := newFakeStore(stored)
		st.delErr = errors.New(leakText)

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN13, "")

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (body %q)", rec.Code, rec.Body.String())
		}
		assertErrorEnvelope(t, rec.Body.String(), "unavailable")
		if _, ok := st.books[validISBN13]; !ok {
			t.Errorf("failed delete removed the book")
		}
	})

	t.Run("isbn-10 path deletes canonical isbn-13", func(t *testing.T) {
		st := newFakeStore(stored)

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN10, "")

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if st.lastDeleteISBN != validISBN13 {
			t.Errorf("store received isbn %q, want canonical %q", st.lastDeleteISBN, validISBN13)
		}
		if _, ok := st.books[validISBN13]; ok {
			t.Errorf("book %q still in store after delete", validISBN13)
		}
	})

	t.Run("request body is ignored", func(t *testing.T) {
		st := newFakeStore(stored)

		rec := serve(t, New(st), http.MethodDelete, "/books/"+validISBN13, `{"title":"ignored"}`)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", rec.Body.String())
		}
	})
}

func TestPutStoresCanonicalISBN(t *testing.T) {
	st := newFakeStore()
	h := New(st)

	req := httptest.NewRequest(http.MethodPut, "/books/"+validISBN10, strings.NewReader(validBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if st.puts != 1 {
		t.Fatalf("store received %d writes, want 1", st.puts)
	}
	if st.lastPut.ISBN != validISBN13 {
		t.Errorf("stored ISBN = %q, want canonical %q", st.lastPut.ISBN, validISBN13)
	}

	var got bookResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.ISBN != validISBN13 {
		t.Errorf("response ISBN = %q, want %q", got.ISBN, validISBN13)
	}
	if got != toResponse(stored) {
		t.Errorf("response = %+v, want %+v", got, toResponse(stored))
	}
}
