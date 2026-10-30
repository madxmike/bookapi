package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"example.com/bookapi/httpapi"
	"example.com/bookapi/store"
)

// These tests close property book.delete_effective_and_idempotent against a
// real Postgres. They are skipped unless BOOKAPI_TEST_DSN is set; with it
// unset the property is assumed, not tested.
//
// ISBNs are distinct per test so a leftover row from an interrupted run
// cannot explain a pass or a failure.

const (
	// 9780131103627 and its ISBN-10 form 0131103628 name the same book.
	deleteISBN13 = "9780131103627"
	deleteISBN10 = "0131103628"

	// A different valid ISBN-13, used by the concurrency test.
	concurrentDeleteISBN = "9780306406157"

	deleteBody = `{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}`
)

func newDeleteFixture(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()

	dsn := os.Getenv("BOOKAPI_TEST_DSN")
	if dsn == "" {
		t.Skip("BOOKAPI_TEST_DSN is not set; skipping Postgres integration test")
	}

	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return httpapi.New(st), st
}

// trackBook removes isbn when the test ends. It runs before the store is
// closed because cleanups are last-in-first-out.
func trackBook(t *testing.T, st *store.Store, isbn string) {
	t.Helper()
	t.Cleanup(func() {
		if err := st.Delete(context.Background(), isbn); err != nil {
			t.Errorf("cleanup delete %s: %v", isbn, err)
		}
	})
}

func serve(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// requireNoContent asserts the 204 contract: no body and no content type.
func requireNoContent(t *testing.T, what string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%s: status = %d, want 204 (body %q)", what, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("%s: body = %q, want empty", what, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("%s: Content-Type = %q, want none", what, ct)
	}
}

// requireGone reads through the store, independently of the handler, so a
// handler that merely claims success cannot satisfy it. ISBN is the primary
// key, so ErrNotFound means the table holds no row for it.
func requireGone(t *testing.T, st *store.Store, isbn string) {
	t.Helper()
	_, err := st.Get(context.Background(), isbn)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get %s after delete = %v, want %v", isbn, err, store.ErrNotFound)
	}
}

// D1, D2, D4: delete removes the row, a repeat delete is still 204, and a
// following GET is 404 not_found.
func TestDeleteIntegrationEffectAndIdempotence(t *testing.T) {
	h, st := newDeleteFixture(t)
	trackBook(t, st, deleteISBN13)

	if rec := serve(t, h, http.MethodPut, "/books/"+deleteISBN13, deleteBody); rec.Code != http.StatusOK {
		t.Fatalf("put: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if _, err := st.Get(context.Background(), deleteISBN13); err != nil {
		t.Fatalf("get after put: %v", err)
	}

	requireNoContent(t, "first delete", serve(t, h, http.MethodDelete, "/books/"+deleteISBN13, ""))
	requireGone(t, st, deleteISBN13)

	// The repeat must not branch on prior existence.
	requireNoContent(t, "second delete", serve(t, h, http.MethodDelete, "/books/"+deleteISBN13, ""))
	requireGone(t, st, deleteISBN13)

	rec := serve(t, h, http.MethodGet, "/books/"+deleteISBN13, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("get error code = %q, want not_found", body.Error.Code)
	}
}

// D5: deleting through the ISBN-10 form removes the canonical ISBN-13 row.
func TestDeleteIntegrationISBN10(t *testing.T) {
	h, st := newDeleteFixture(t)
	trackBook(t, st, deleteISBN13)

	if rec := serve(t, h, http.MethodPut, "/books/"+deleteISBN13, deleteBody); rec.Code != http.StatusOK {
		t.Fatalf("put: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if _, err := st.Get(context.Background(), deleteISBN13); err != nil {
		t.Fatalf("get after put: %v", err)
	}

	requireNoContent(t, "delete via isbn-10", serve(t, h, http.MethodDelete, "/books/"+deleteISBN10, ""))
	requireGone(t, st, deleteISBN13)

	for _, form := range []string{deleteISBN13, deleteISBN10} {
		if rec := serve(t, h, http.MethodGet, "/books/"+form, ""); rec.Code != http.StatusNotFound {
			t.Errorf("get %s after delete: status = %d, want 404 (body %q)", form, rec.Code, rec.Body.String())
		}
	}
}

// D7: 16 concurrent deletes of one ISBN all return 204 and the row ends up
// deleted.
func TestDeleteIntegrationConcurrent(t *testing.T) {
	h, st := newDeleteFixture(t)
	trackBook(t, st, concurrentDeleteISBN)

	if rec := serve(t, h, http.MethodPut, "/books/"+concurrentDeleteISBN, deleteBody); rec.Code != http.StatusOK {
		t.Fatalf("put: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if _, err := st.Get(context.Background(), concurrentDeleteISBN); err != nil {
		t.Fatalf("get after put: %v", err)
	}

	const deleters = 16
	codes := make([]int, deleters)
	bodies := make([]string, deleters)
	var wg sync.WaitGroup
	for i := 0; i < deleters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodDelete, "/books/"+concurrentDeleteISBN, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusNoContent {
			t.Errorf("delete %d: status = %d, want 204 (body %q)", i, code, bodies[i])
		}
	}
	requireGone(t, st, concurrentDeleteISBN)
}
