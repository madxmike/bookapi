// Package httpapi exposes the book catalog over HTTP.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"example.com/bookapi/internal/book"
	"example.com/bookapi/internal/isbn"
	"example.com/bookapi/internal/store"
)

const (
	maxBodyBytes = 1 << 20
	storeTimeout = time.Second
)

// Store is the persistence the handlers need.
type Store interface {
	Get(ctx context.Context, isbn string) (book.Book, error)
	Put(ctx context.Context, b book.Book) error
	Delete(ctx context.Context, isbn string) error
}

// New returns the handler serving the books API.
func New(st Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /books/{isbn}", func(w http.ResponseWriter, r *http.Request) {
		handlePut(w, r, st)
	})
	mux.HandleFunc("GET /books/{isbn}", func(w http.ResponseWriter, r *http.Request) {
		handleGet(w, r, st)
	})
	mux.HandleFunc("DELETE /books/{isbn}", func(w http.ResponseWriter, r *http.Request) {
		handleDelete(w, r, st)
	})
	mux.HandleFunc("/", handleNotFound)
	return mux
}

func handlePut(w http.ResponseWriter, r *http.Request, st Store) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_isbn", "isbn is not valid")
		return
	}

	in, ok := decodeInput(w, r)
	if !ok {
		return
	}

	b := book.Book{
		ISBN:          canonical,
		Title:         in.Title,
		Author:        in.Author,
		PublishedYear: in.PublishedYear,
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := st.Put(ctx, b); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the book store is unavailable")
		return
	}

	writeJSON(w, http.StatusOK, toResponse(b))
}

func handleGet(w http.ResponseWriter, r *http.Request, st Store) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_isbn", "isbn is not valid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	b, err := st.Get(ctx, canonical)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no book with that isbn")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the book store is unavailable")
		return
	}

	writeJSON(w, http.StatusOK, toResponse(b))
}

func handleDelete(w http.ResponseWriter, r *http.Request, st Store) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_isbn", "isbn is not valid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := st.Delete(ctx, canonical); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the book store is unavailable")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no such route")
}

type bookInput struct {
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedYear int    `json:"published_year"`
}

func decodeInput(w http.ResponseWriter, r *http.Request) (bookInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var in bookInput
	if err := dec.Decode(&in); err != nil {
		writeDecodeError(w, err)
		return bookInput{}, false
	}

	// Reject a second JSON value, and read past any trailing bytes so an
	// oversized body is reported as such even when the first value fits
	// inside the limit.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		writeDecodeError(w, err)
		return bookInput{}, false
	}

	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Author) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "title and author must not be empty")
		return bookInput{}, false
	}

	return in, true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds 1 MiB")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_body", "request body must be a JSON object with title, author, and published_year")
}

type bookResponse struct {
	ISBN          string `json:"isbn"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedYear int    `json:"published_year"`
}

func toResponse(b book.Book) bookResponse {
	return bookResponse{
		ISBN:          b.ISBN,
		Title:         b.Title,
		Author:        b.Author,
		PublishedYear: b.PublishedYear,
	}
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
