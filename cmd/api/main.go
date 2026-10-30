// Command api serves the book catalog HTTP API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/bookapi/internal/httpapi"
	"example.com/bookapi/internal/store"
)

func main() {
	addr := os.Getenv("BOOKAPI_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	dsn := os.Getenv("BOOKAPI_DSN")
	if dsn == "" {
		log.Fatal("BOOKAPI_DSN is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	st, err := store.Open(openCtx, dsn)
	cancel()
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = st.Migrate(migrateCtx)
	cancel()
	if err != nil {
		log.Fatalf("migrate store: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newMux(st.Ping, st),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}

func newMux(ready func(context.Context) error, st httpapi.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("GET /readyz", handleReadyz(ready))
	mux.Handle("/", httpapi.New(st))
	return mux
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleReadyz(ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
