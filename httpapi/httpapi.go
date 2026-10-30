// Package httpapi exposes the book catalog over HTTP.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"example.com/bookapi/book"
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
	List(ctx context.Context, author *string) ([]book.Book, error)
}

// handler is the semantic handler type. A handler can only write through the
// response boundary, so no route can bypass the common headers or the stable
// error envelope.
type handler func(*response, *http.Request)

// route is one entry in the route table. Method is empty for the catch-all
// route.
type route struct {
	method  string
	pattern string
	handler handler
}

// path is the ServeMux pattern for the route.
func (rt route) path() string {
	if rt.method == "" {
		return rt.pattern
	}
	return rt.method + " " + rt.pattern
}

type server struct {
	store Store
	ready func(context.Context) error
	mux   *http.ServeMux
}

// New returns the handler serving every route, including health and
// readiness. A readiness probe may be supplied; otherwise a store that can
// ping is used, and a store with no ping is treated as always ready. The
// fallback keeps handlers built around an in-memory store usable without a
// database.
func New(st Store, ready ...func(context.Context) error) http.Handler {
	s := &server{
		store: st,
		ready: func(context.Context) error { return nil },
	}
	switch {
	case len(ready) > 0 && ready[0] != nil:
		s.ready = ready[0]
	case st != nil:
		if pinger, ok := st.(interface{ Ping(context.Context) error }); ok {
			s.ready = pinger.Ping
		}
	}
	s.mux = s.buildMux()
	return s
}

// ServeHTTP is the single response boundary. Every route is served through
// it: the common headers are applied once, the body is buffered so HEAD can
// mirror GET, and a panic becomes the stable error envelope. It is the one
// handler-shaped function that is not a route: it is the table's dispatcher.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	res := &response{
		header: make(http.Header),
		status: http.StatusOK,
		head:   r.Method == http.MethodHead,
	}
	defer func() {
		if p := recover(); p != nil {
			res.fail(failureInternal)
		}
		res.flush(w)
	}()
	s.mux.ServeHTTP(&wire{res: res}, r)
}

// buildMux attaches the route table. Registration is only table-driven: the
// discovery check rejects any route registered another way.
func (s *server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.Handle(rt.path(), adapt(rt.handler))
	}
	return mux
}

// adapt lets the mux call a semantic handler. It refuses to run outside the
// response boundary, so a handler cannot be served with a raw writer.
func adapt(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, ok := w.(interface{ target() *response })
		if !ok {
			panic("httpapi: route served outside the response boundary")
		}
		h(res.target(), r)
	}
}

// routes is the route table. Every handler-shaped function in the module is
// referenced here, and buildMux is the only place a route is registered.
func (s *server) routes() []route {
	return []route{
		route{method: http.MethodGet, pattern: "/healthz", handler: s.healthz},
		route{method: http.MethodGet, pattern: "/readyz", handler: s.readyz},
		route{method: http.MethodGet, pattern: "/books", handler: s.list},
		route{method: http.MethodPut, pattern: "/books/{isbn}", handler: s.put},
		route{method: http.MethodGet, pattern: "/books/{isbn}", handler: s.get},
		route{method: http.MethodHead, pattern: "/books/{isbn}", handler: s.get},
		route{method: http.MethodDelete, pattern: "/books/{isbn}", handler: s.delete},
		route{method: "", pattern: "/", handler: s.notFound},
	}
}
