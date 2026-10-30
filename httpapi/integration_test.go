package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/bookapi/httpapi"
	"example.com/bookapi/store"
)

const (
	// 9780131103627 and its ISBN-10 form 0131103628 name the same book.
	deleteISBN13 = "9780131103627"
	deleteISBN10 = "0131103628"
	invalidISBN  = "978013110362" // bad check digit

	deleteBody = `{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}`
)

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("BOOKAPI_TEST_DSN")
	if dsn == "" {
		t.Skip("BOOKAPI_TEST_DSN is not set; skipping Postgres integration test")
	}
	return dsn
}

func openStore(t *testing.T, dsn string) *store.Store {
	t.Helper()
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
	return st
}

func newFixture(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	dsn := integrationDSN(t)
	st := openStore(t, dsn)
	return httpapi.New(st), st, dsn
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

// uniqueSeq makes identifiers collision-free within this test binary. The
// random base keeps parallel test binaries from overlapping.
var uniqueSeq atomic.Uint64

var uniqueBase = func() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return binary.BigEndian.Uint64(b[:]) % 1_000_000_000
}()

func uniqueID() uint64 {
	return (uniqueBase + uniqueSeq.Add(1)) % 1_000_000_000
}

// uniqueISBN returns a valid ISBN-13 that does not collide with other tests.
func uniqueISBN(t *testing.T) string {
	t.Helper()
	first12 := fmt.Sprintf("978%09d", uniqueID())
	sum := 0
	for i := 0; i < len(first12); i++ {
		d := int(first12[i] - '0')
		if i%2 == 0 {
			sum += d
		} else {
			sum += 3 * d
		}
	}
	return fmt.Sprintf("%s%d", first12, (10-sum%10)%10)
}

func serve(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func requireStatus(t *testing.T, what string, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d, want %d (body %q)", what, rec.Code, want, rec.Body.String())
	}
}

// requireNoContent asserts the 204 contract: no body and no content type.
func requireNoContent(t *testing.T, what string, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireStatus(t, what, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("%s: body = %q, want empty", what, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("%s: Content-Type = %q, want none", what, ct)
	}
}

// requireCount reads COUNT(*) independently of the handler, so a handler that
// merely claims success cannot satisfy it.
func requireCount(t *testing.T, st *store.Store, isbn string, want int) {
	t.Helper()
	n, err := st.Count(context.Background(), isbn)
	if err != nil {
		t.Fatalf("count %s: %v", isbn, err)
	}
	if n != want {
		t.Fatalf("rows for %s = %d, want %d", isbn, n, want)
	}
}

func requireGone(t *testing.T, st *store.Store, isbn string) {
	t.Helper()
	requireCount(t, st, isbn, 0)
}

type bookJSON struct {
	ISBN          string `json:"isbn"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedYear int    `json:"published_year"`
}

func decodeBook(t *testing.T, rec *httptest.ResponseRecorder) bookJSON {
	t.Helper()
	var b bookJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode book body %q: %v", rec.Body.String(), err)
	}
	return b
}

func requireStoredBook(t *testing.T, st *store.Store, isbn string, want bookJSON) {
	t.Helper()
	got, err := st.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get stored record %s: %v", isbn, err)
	}
	if got.ISBN != want.ISBN || got.Title != want.Title || got.Author != want.Author || got.PublishedYear != want.PublishedYear {
		t.Errorf("stored record = %+v, want %+v", got, want)
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

func requireErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	body := rec.Body.String()

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	if len(top) != 1 {
		t.Fatalf("error body has keys %v, want only \"error\": %s", top, body)
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
		t.Fatalf("error detail has keys %v, want exactly code and message: %s", detail, body)
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
		t.Errorf("error message = %q, want one of the stable messages for %q: %v", message, code, allowed)
	}
}

// Scenario 9: a write through one instance is visible through another.
func TestPutGetAcrossInstances(t *testing.T) {
	first, st, dsn := newFixture(t)
	second := httpapi.New(openStore(t, dsn))
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	requireStatus(t, "put", serve(t, first, http.MethodPut, "/books/"+isbn, deleteBody), http.StatusOK)
	rec := serve(t, second, http.MethodGet, "/books/"+isbn, "")
	requireStatus(t, "get on second instance", rec, http.StatusOK)

	got := decodeBook(t, rec)
	if got.ISBN != isbn || got.Title != "The C Programming Language" {
		t.Errorf("get = %+v, want isbn %s and the written title", got, isbn)
	}
	requireCount(t, st, isbn, 1)
}

// Scenario 8: a record survives closing and reopening the store, which is the
// process-lifetime part of a restart.
func TestPutGetAcrossRestart(t *testing.T) {
	dsn := integrationDSN(t)
	st := openStore(t, dsn)
	isbn := uniqueISBN(t)

	h := httpapi.New(st)
	requireStatus(t, "put", serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody), http.StatusOK)
	requireCount(t, st, isbn, 1)

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened := openStore(t, dsn)
	trackBook(t, reopened, isbn)
	rec := serve(t, httpapi.New(reopened), http.MethodGet, "/books/"+isbn, "")
	requireStatus(t, "get after reopen", rec, http.StatusOK)
	if got := decodeBook(t, rec); got.ISBN != isbn {
		t.Errorf("get after reopen = %+v, want isbn %s", got, isbn)
	}
}

// Scenarios 5 and 6: identical PUTs are stable, and a later PUT updates the
// same row.
func TestPutIsIdempotentAndLastWriteWins(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	first := serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody)
	requireStatus(t, "first put", first, http.StatusOK)
	firstBook := decodeBook(t, first)

	second := serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody)
	requireStatus(t, "second put", second, http.StatusOK)
	secondBook := decodeBook(t, second)
	if secondBook != firstBook {
		t.Errorf("second put = %+v, want identical to %+v", secondBook, firstBook)
	}
	requireCount(t, st, isbn, 1)
	requireStoredBook(t, st, isbn, firstBook)

	updated := `{"title":"Updated Title","author":"Updated Author","published_year":1999}`
	third := serve(t, h, http.MethodPut, "/books/"+isbn, updated)
	requireStatus(t, "third put", third, http.StatusOK)
	if got := decodeBook(t, third); got.Title != "Updated Title" || got.PublishedYear != 1999 {
		t.Errorf("third put = %+v, want the updated record", got)
	}
	requireCount(t, st, isbn, 1)
	requireStoredBook(t, st, isbn, bookJSON{
		ISBN:          isbn,
		Title:         "Updated Title",
		Author:        "Updated Author",
		PublishedYear: 1999,
	})
}

// Scenario 7: 32 concurrent writes of one ISBN leave exactly one row.
func TestConcurrentPutsOneRow(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	const writers = 32
	codes := make([]int, writers)
	bodies := make([]string, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"title":"title-%d","author":"author-%d","published_year":%d}`, i, i, 2000+i)
			req := httptest.NewRequest(http.MethodPut, "/books/"+isbn, strings.NewReader(body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code < 200 || code > 299 {
			t.Errorf("put %d: status = %d, want 2xx (body %q)", i, code, bodies[i])
		}
	}
	requireCount(t, st, isbn, 1)
}

// Scenario 11: the ISBN-10 and ISBN-13 forms address the same record.
func TestGetViaISBN10(t *testing.T) {
	h, st, _ := newFixture(t)
	trackBook(t, st, deleteISBN13)

	requireStatus(t, "put isbn-13", serve(t, h, http.MethodPut, "/books/"+deleteISBN13, deleteBody), http.StatusOK)

	by13 := serve(t, h, http.MethodGet, "/books/"+deleteISBN13, "")
	requireStatus(t, "get isbn-13", by13, http.StatusOK)
	by10 := serve(t, h, http.MethodGet, "/books/"+deleteISBN10, "")
	requireStatus(t, "get isbn-10", by10, http.StatusOK)

	if got13, got10 := decodeBook(t, by13), decodeBook(t, by10); got13 != got10 {
		t.Errorf("isbn-13 record = %+v, isbn-10 record = %+v, want the same record", got13, got10)
	}
}

// Scenarios 13-16: delete removes the row, repeats are 204, and a later GET is
// 404. The count is read through the store, not the handler.
func TestDeleteIntegrationEffectAndIdempotence(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	requireStatus(t, "put", serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody), http.StatusOK)
	requireCount(t, st, isbn, 1)

	requireNoContent(t, "first delete", serve(t, h, http.MethodDelete, "/books/"+isbn, ""))
	requireGone(t, st, isbn)

	// The repeat must not branch on prior existence.
	requireNoContent(t, "second delete", serve(t, h, http.MethodDelete, "/books/"+isbn, ""))
	requireGone(t, st, isbn)

	rec := serve(t, h, http.MethodGet, "/books/"+isbn, "")
	requireStatus(t, "get after delete", rec, http.StatusNotFound)
	requireErrorEnvelope(t, rec, "not_found")
}

// Scenario 15: deleting a valid, never-stored ISBN is a success and creates
// no row.
func TestDeleteIntegrationAbsent(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	requireNoContent(t, "delete absent", serve(t, h, http.MethodDelete, "/books/"+isbn, ""))
	requireGone(t, st, isbn)
}

// Scenario 17: deleting through the ISBN-10 form removes the canonical
// ISBN-13 row, and both forms then read 404.
func TestDeleteIntegrationISBN10(t *testing.T) {
	h, st, _ := newFixture(t)
	trackBook(t, st, deleteISBN13)

	requireStatus(t, "put", serve(t, h, http.MethodPut, "/books/"+deleteISBN13, deleteBody), http.StatusOK)
	requireCount(t, st, deleteISBN13, 1)

	requireNoContent(t, "delete via isbn-10", serve(t, h, http.MethodDelete, "/books/"+deleteISBN10, ""))
	requireGone(t, st, deleteISBN13)

	for _, form := range []string{deleteISBN13, deleteISBN10} {
		if rec := serve(t, h, http.MethodGet, "/books/"+form, ""); rec.Code != http.StatusNotFound {
			t.Errorf("get %s after delete: status = %d, want 404 (body %q)", form, rec.Code, rec.Body.String())
		}
	}
}

// Scenario 18: an invalid ISBN is rejected before storage, and the stored row
// is untouched.
func TestDeleteIntegrationInvalidLeavesRow(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	requireStatus(t, "put", serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody), http.StatusOK)
	requireCount(t, st, isbn, 1)

	rec := serve(t, h, http.MethodDelete, "/books/"+invalidISBN, "")
	requireStatus(t, "delete invalid", rec, http.StatusBadRequest)
	requireErrorEnvelope(t, rec, "invalid_isbn")
	requireCount(t, st, isbn, 1)
}

// Scenario 19: concurrent deletes all succeed and leave no row.
func TestDeleteIntegrationConcurrent(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)

	requireStatus(t, "put", serve(t, h, http.MethodPut, "/books/"+isbn, deleteBody), http.StatusOK)
	requireCount(t, st, isbn, 1)

	const deleters = 16
	codes := make([]int, deleters)
	bodies := make([]string, deleters)
	var wg sync.WaitGroup
	for i := 0; i < deleters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodDelete, "/books/"+isbn, nil)
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
	requireGone(t, st, isbn)
}

// Scenario 12: a 2 MiB PUT body is rejected with 413 within one second.
func TestBodyLimitIntegration(t *testing.T) {
	h, _, _ := newFixture(t)
	isbn := uniqueISBN(t)
	body := `{"title":"` + strings.Repeat("a", 2<<20) + `","author":"a","published_year":1}`

	start := time.Now()
	rec := serve(t, h, http.MethodPut, "/books/"+isbn, body)
	elapsed := time.Since(start)

	requireStatus(t, "oversized put", rec, http.StatusRequestEntityTooLarge)
	requireErrorEnvelope(t, rec, "payload_too_large")
	if elapsed > time.Second {
		t.Errorf("oversized put took %v, want at most 1s", elapsed)
	}
}
