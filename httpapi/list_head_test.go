package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	// The fake intentionally returns an unsorted slice: ordering is owned by
	// storage (D18), and the Postgres integration test with scrambled inserts
	// plus store/store_list_test.go are the ordering evidence. This test only
	// checks that the handler returns exactly the store's books.
	t.Run("returns every stored book", func(t *testing.T) {
		h := New(newFakeStore(third, first, second))
		rec := do(t, h, http.MethodGet, "/books", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got []bookResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		want := map[string]bookResponse{
			first.ISBN:  toResponse(first),
			second.ISBN: toResponse(second),
			third.ISBN:  toResponse(third),
		}
		if len(got) != len(want) {
			t.Fatalf("books = %+v, want %+v", got, want)
		}
		seen := map[string]bool{}
		for _, b := range got {
			if seen[b.ISBN] {
				t.Errorf("duplicate book %s", b.ISBN)
			}
			seen[b.ISBN] = true
			if want[b.ISBN] != b {
				t.Errorf("book %+v, want %+v", b, want[b.ISBN])
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
		want := map[string]bookResponse{
			first.ISBN: toResponse(first),
			third.ISBN: toResponse(third),
		}
		if len(got) != len(want) {
			t.Fatalf("books = %+v, want %+v", got, want)
		}
		seen := map[string]bool{}
		for _, b := range got {
			if seen[b.ISBN] {
				t.Errorf("duplicate book %s", b.ISBN)
			}
			seen[b.ISBN] = true
			if want[b.ISBN] != b {
				t.Errorf("book %+v, want %+v", b, want[b.ISBN])
			}
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

type panicStore struct{ *fakeStore }

func (panicStore) Get(context.Context, string) (book.Book, error) {
	panic("boom")
}

// TestHeadMirrorsEveryGetStatus runs a real net/http server so HEAD body
// suppression is observable, and covers every GET status the read route can
// produce: 200, 400, 404, 503, and 500 (a recovered panic).
func TestHeadMirrorsEveryGetStatus(t *testing.T) {
	leaky := newFakeStore(stored)
	leaky.getErr = errors.New(leakText)

	tests := []struct {
		name  string
		path  string
		store Store
		want  int
	}{
		{name: "200", path: "/books/" + validISBN13, store: newFakeStore(stored), want: http.StatusOK},
		{name: "400", path: "/books/" + invalidISBN, store: newFakeStore(), want: http.StatusBadRequest},
		{name: "404", path: "/books/9780306406157", store: newFakeStore(), want: http.StatusNotFound},
		{name: "503", path: "/books/" + validISBN13, store: leaky, want: http.StatusServiceUnavailable},
		{name: "500", path: "/books/" + validISBN13, store: panicStore{newFakeStore()}, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(New(tt.store))
			defer srv.Close()

			do := func(method string) (*http.Response, []byte) {
				t.Helper()
				req, err := http.NewRequest(method, srv.URL+tt.path, nil)
				if err != nil {
					t.Fatalf("build %s: %v", method, err)
				}
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatalf("%s: %v", method, err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read %s: %v", method, err)
				}
				return resp, body
			}

			get, getBody := do(http.MethodGet)
			head, headBody := do(http.MethodHead)

			if get.StatusCode != tt.want {
				t.Fatalf("GET status = %d, want %d (body %q)", get.StatusCode, tt.want, getBody)
			}
			if head.StatusCode != tt.want {
				t.Errorf("HEAD status = %d, want %d (body %q)", head.StatusCode, tt.want, headBody)
			}
			if len(getBody) == 0 {
				t.Fatal("GET body is empty; HEAD cannot be shown to mirror it")
			}
			if len(headBody) != 0 {
				t.Errorf("HEAD body = %q, want empty", headBody)
			}
			if head.ContentLength != int64(len(getBody)) {
				t.Errorf("HEAD ContentLength = %d, want %d", head.ContentLength, len(getBody))
			}

			getHeader := get.Header.Clone()
			headHeader := head.Header.Clone()
			// Date is generated by net/http and can differ between requests.
			getHeader.Del("Date")
			headHeader.Del("Date")
			if !reflect.DeepEqual(getHeader, headHeader) {
				t.Errorf("HEAD headers = %v, GET headers = %v", headHeader, getHeader)
			}

			if tt.want >= 400 {
				assertEnvelope(t, string(getBody))
			}
		})
	}
}
