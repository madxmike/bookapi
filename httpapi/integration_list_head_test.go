package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
)

func uniqueAuthor(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("it-author-%d-%d", uniqueBase, uniqueSeq.Add(1))
}

func putBook(t *testing.T, h http.Handler, isbn, author string) bookJSON {
	t.Helper()
	body := fmt.Sprintf(`{"title":"Title %s","author":"%s","published_year":2020}`, isbn, author)
	rec := serve(t, h, http.MethodPut, "/books/"+isbn, body)
	requireStatus(t, "put "+isbn, rec, http.StatusOK)
	return decodeBook(t, rec)
}

// Scenario 23: GET /books returns a JSON array ordered by ISBN.
func TestListIntegrationOrdered(t *testing.T) {
	h, st, _ := newFixture(t)
	author := uniqueAuthor(t)
	isbns := []string{uniqueISBN(t), uniqueISBN(t)}
	sort.Strings(isbns)
	low, high := isbns[0], isbns[1]
	for _, isbn := range []string{low, high} {
		isbn := isbn
		trackBook(t, st, isbn)
	}

	// Write the higher ISBN first so order cannot be insertion order.
	putBook(t, h, high, author)
	putBook(t, h, low, author)

	rec := serve(t, h, http.MethodGet, "/books?author="+author, "")
	requireStatus(t, "list", rec, http.StatusOK)

	var got []bookJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list = %+v, want the two books for %q", got, author)
	}
	if got[0].ISBN != low || got[1].ISBN != high {
		t.Errorf("list = [%s, %s], want [%s, %s] ordered by isbn", got[0].ISBN, got[1].ISBN, low, high)
	}
}

// Scenario 24: an optional author query filters to an exact match.
func TestListIntegrationAuthorFilter(t *testing.T) {
	h, st, _ := newFixture(t)
	author := uniqueAuthor(t)
	other := uniqueAuthor(t)
	first := uniqueISBN(t)
	second := uniqueISBN(t)
	third := uniqueISBN(t)
	for _, isbn := range []string{first, second, third} {
		isbn := isbn
		trackBook(t, st, isbn)
	}

	putBook(t, h, first, author)
	putBook(t, h, second, author)
	putBook(t, h, third, other)

	rec := serve(t, h, http.MethodGet, "/books?author="+author, "")
	requireStatus(t, "list by author", rec, http.StatusOK)
	var got []bookJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list by author = %+v, want two books", got)
	}
	for _, b := range got {
		if b.Author != author {
			t.Errorf("book %s has author %q, want %q", b.ISBN, b.Author, author)
		}
	}

	// Scenario 25: no matches is 200 [].
	rec = serve(t, h, http.MethodGet, "/books?author="+uniqueAuthor(t), "")
	requireStatus(t, "list no matches", rec, http.StatusOK)
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("no-match body = %q, want []", body)
	}
}

// Scenarios 26 and 27: HEAD mirrors GET for a stored and a missing book.
func TestHeadIntegrationMirrorsGet(t *testing.T) {
	h, st, _ := newFixture(t)
	isbn := uniqueISBN(t)
	trackBook(t, st, isbn)
	putBook(t, h, isbn, uniqueAuthor(t))

	compare := func(t *testing.T, path string) {
		t.Helper()
		get := serve(t, h, http.MethodGet, path, "")
		head := serve(t, h, http.MethodHead, path, "")

		if head.Code != get.Code {
			t.Errorf("%s: HEAD status = %d, GET status = %d", path, head.Code, get.Code)
		}
		for _, key := range []string{"Content-Type", "Content-Length", "X-Content-Type-Options", "Cache-Control"} {
			if got, want := head.Header().Get(key), get.Header().Get(key); got != want {
				t.Errorf("%s: HEAD %s = %q, GET %s = %q", path, key, got, key, want)
			}
		}
		if head.Body.Len() != 0 {
			t.Errorf("%s: HEAD body = %q, want empty", path, head.Body.String())
		}
		if get.Body.Len() == 0 {
			t.Errorf("%s: GET body is empty; HEAD cannot mirror it", path)
		}
	}

	t.Run("stored", func(t *testing.T) { compare(t, "/books/"+isbn) })
	t.Run("missing", func(t *testing.T) { compare(t, "/books/"+uniqueISBN(t)) })
}
