package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"example.com/bookapi/book"
	"example.com/bookapi/isbn"
	"example.com/bookapi/store"
)

func (s *server) healthz(w *response, r *http.Request) {
	w.text(http.StatusOK, "ok")
}

func (s *server) readyz(w *response, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		w.fail(failureUnavailable)
		return
	}
	w.text(http.StatusOK, "ok")
}

func (s *server) notFound(w *response, r *http.Request) {
	w.fail(failureRouteNotFound)
}

func (s *server) put(w *response, r *http.Request) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		w.fail(failureInvalidISBN)
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
	if err := s.store.Put(ctx, b); err != nil {
		w.fail(failureUnavailable)
		return
	}

	w.json(http.StatusOK, toResponse(b))
}

func (s *server) get(w *response, r *http.Request) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		w.fail(failureInvalidISBN)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	b, err := s.store.Get(ctx, canonical)
	if errors.Is(err, store.ErrNotFound) {
		w.fail(failureBookNotFound)
		return
	}
	if err != nil {
		w.fail(failureUnavailable)
		return
	}

	w.json(http.StatusOK, toResponse(b))
}

func (s *server) delete(w *response, r *http.Request) {
	canonical, err := isbn.Canonical(r.PathValue("isbn"))
	if err != nil {
		w.fail(failureInvalidISBN)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	// An absent row is success. A store that reports absence explicitly must
	// not turn an idempotent DELETE into a 404 or a 503.
	if err := s.store.Delete(ctx, canonical); err != nil && !errors.Is(err, store.ErrNotFound) {
		w.fail(failureUnavailable)
		return
	}

	w.noContent()
}

func (s *server) list(w *response, r *http.Request) {
	var author *string
	if values, ok := r.URL.Query()["author"]; ok {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		author = &value
	}

	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	books, err := s.store.List(ctx, author)
	if err != nil {
		w.fail(failureUnavailable)
		return
	}

	out := make([]bookResponse, 0, len(books))
	for _, b := range books {
		out = append(out, toResponse(b))
	}
	w.json(http.StatusOK, out)
}

type bookInput struct {
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedYear int    `json:"published_year"`
}

func decodeInput(w *response, r *http.Request) (bookInput, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		w.fail(failureInvalidBody)
		return bookInput{}, false
	}
	if len(raw) > maxBodyBytes {
		w.fail(failurePayloadTooLarge)
		return bookInput{}, false
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var in bookInput
	if err := dec.Decode(&in); err != nil {
		w.fail(failureInvalidBody)
		return bookInput{}, false
	}

	// Reject a second JSON value. Trailing whitespace is fine.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		w.fail(failureInvalidBody)
		return bookInput{}, false
	}

	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Author) == "" {
		w.fail(failureEmptyFields)
		return bookInput{}, false
	}

	return in, true
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
