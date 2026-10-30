package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRealServerHeadAndList runs the handler behind net/http, not just an
// httptest.ResponseRecorder, so HEAD body suppression and Content-Length are
// exercised at the real server boundary.
func TestRealServerHeadAndList(t *testing.T) {
	srv := httptest.NewServer(New(newFakeStore(stored)))
	defer srv.Close()
	client := srv.Client()

	do := func(method, path string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("build %s %s: %v", method, path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s %s: %v", method, path, err)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q", method, path, got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q", method, path, got)
		}
		return resp, body
	}

	list, listBody := do(http.MethodGet, "/books")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("GET /books status = %d, want 200", list.StatusCode)
	}
	var books []bookResponse
	if err := json.Unmarshal(listBody, &books); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(books) != 1 || books[0].ISBN != validISBN13 {
		t.Errorf("GET /books = %+v, want the stored book", books)
	}

	get, getBody := do(http.MethodGet, "/books/"+validISBN13)
	head, headBody := do(http.MethodHead, "/books/"+validISBN13)

	if head.StatusCode != get.StatusCode {
		t.Errorf("HEAD status = %d, GET status = %d", head.StatusCode, get.StatusCode)
	}
	if len(headBody) != 0 {
		t.Errorf("HEAD body = %q, want empty", headBody)
	}
	if got, want := head.ContentLength, int64(len(getBody)); got != want {
		t.Errorf("HEAD ContentLength = %d, want %d", got, want)
	}
	if got, want := head.Header.Get("Content-Length"), get.Header.Get("Content-Length"); got != want {
		t.Errorf("HEAD Content-Length = %q, GET = %q", got, want)
	}
	if got, want := head.Header.Get("Content-Type"), get.Header.Get("Content-Type"); got != want {
		t.Errorf("HEAD Content-Type = %q, GET = %q", got, want)
	}

	missing, missingBody := do(http.MethodHead, "/books/9780306406157")
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD missing status = %d, want 404", missing.StatusCode)
	}
	if len(missingBody) != 0 {
		t.Errorf("HEAD missing body = %q, want empty", missingBody)
	}
	if ct := missing.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("HEAD missing Content-Type = %q, want application/json", ct)
	}
	if cl := missing.Header.Get("Content-Length"); cl == "" || cl == "0" {
		t.Errorf("HEAD missing Content-Length = %q, want the envelope length", cl)
	}
}
