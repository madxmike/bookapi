package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// response is the only way a handler can write. Success methods accept only a
// 2xx status and a body they build; fail accepts only a declared failure. An
// error therefore cannot bypass the stable envelope, and the common headers
// are (re)applied by flush at the one boundary.
type response struct {
	header http.Header
	status int
	body   []byte
	head   bool
}

// wire adapts a response to the http.ResponseWriter the mux expects. Handlers
// never receive it.
type wire struct {
	res *response
}

func (w *wire) target() *response { return w.res }

func (w *wire) Header() http.Header { return w.res.header }

func (w *wire) WriteHeader(status int) {
	w.res.status = status
}

func (w *wire) Write(p []byte) (int, error) {
	w.res.body = append(w.res.body, p...)
	return len(p), nil
}

func (w *response) json(status int, v any) {
	if status < 200 || status > 299 {
		panic("httpapi: json status must be 2xx")
	}
	data, err := json.Marshal(v)
	if err != nil {
		w.fail(failureInternal)
		return
	}
	w.send(status, "application/json", data)
}

func (w *response) text(status int, body string) {
	if status < 200 || status > 299 {
		panic("httpapi: text status must be 2xx")
	}
	w.send(status, "text/plain; charset=utf-8", []byte(body))
}

func (w *response) noContent() {
	w.header.Del("Content-Type")
	w.header.Del("Content-Length")
	w.status = http.StatusNoContent
	w.body = nil
}

func (w *response) fail(f failure) {
	if f.status < 400 {
		panic("httpapi: failure status must be 4xx or 5xx")
	}
	data, err := json.Marshal(errorResponse{Error: errorDetail{Code: f.code, Message: f.message}})
	if err != nil {
		// Unreachable for a struct of strings, but keep the envelope intact.
		data = []byte(`{"error":{"code":"internal","message":"internal error"}}`)
	}
	w.header = make(http.Header)
	w.send(f.status, "application/json", data)
}

func (w *response) send(status int, contentType string, body []byte) {
	w.status = status
	w.header.Set("Content-Type", contentType)
	w.header.Set("Content-Length", strconv.Itoa(len(body)))
	w.body = body
}

// flush is the only place that touches the real writer. It applies the
// common headers after copying any handler headers, so they cannot be
// overridden, and it suppresses the body for HEAD while keeping the headers,
// including Content-Length.
func (w *response) flush(out http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			out.Header().Add(key, value)
		}
	}
	if w.status >= 200 && w.status != http.StatusNoContent && w.status != http.StatusNotModified {
		out.Header().Set("Content-Length", strconv.Itoa(len(w.body)))
	}
	out.Header().Set("X-Content-Type-Options", "nosniff")
	out.Header().Set("Cache-Control", "no-store")
	out.WriteHeader(w.status)
	if w.head || w.status == http.StatusNoContent || w.status == http.StatusNotModified {
		return
	}
	_, _ = out.Write(w.body)
}

// failure is a declared error response. Handlers choose one of these values
// rather than composing a status, code, and message themselves.
type failure struct {
	status  int
	code    string
	message string
}

var (
	failureInvalidISBN = failure{
		status:  http.StatusBadRequest,
		code:    "invalid_isbn",
		message: "isbn is not valid",
	}
	failureInvalidBody = failure{
		status:  http.StatusBadRequest,
		code:    "invalid_body",
		message: "request body must be a JSON object with title, author, and published_year",
	}
	failureEmptyFields = failure{
		status:  http.StatusBadRequest,
		code:    "invalid_body",
		message: "title and author must not be empty",
	}
	failurePayloadTooLarge = failure{
		status:  http.StatusRequestEntityTooLarge,
		code:    "payload_too_large",
		message: "request body exceeds 1 MiB",
	}
	failureBookNotFound = failure{
		status:  http.StatusNotFound,
		code:    "not_found",
		message: "no book with that isbn",
	}
	failureRouteNotFound = failure{
		status:  http.StatusNotFound,
		code:    "not_found",
		message: "no such route",
	}
	failureUnavailable = failure{
		status:  http.StatusServiceUnavailable,
		code:    "unavailable",
		message: "the book store is unavailable",
	}
	failureInternal = failure{
		status:  http.StatusInternalServerError,
		code:    "internal",
		message: "internal error",
	}
)

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
