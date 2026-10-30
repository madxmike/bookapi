package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"example.com/bookapi/store"
)

type runningServer struct {
	cmd  *exec.Cmd
	log  *bytes.Buffer
	once sync.Once
}

func (s *runningServer) stop() {
	s.once.Do(func() {
		if s.cmd.Process == nil {
			return
		}
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = s.cmd.Process.Kill()
			<-done
		}
	})
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}
	return addr
}

func startServer(t *testing.T, bin, dsn, addr string) *runningServer {
	t.Helper()
	s := &runningServer{cmd: exec.Command(bin), log: new(bytes.Buffer)}
	s.cmd.Env = append(os.Environ(), "BOOKAPI_DSN="+dsn, "BOOKAPI_ADDR="+addr)
	s.cmd.Stdout = s.log
	s.cmd.Stderr = s.log
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		s.stop()
		if t.Failed() {
			t.Logf("server output:\n%s", s.log.String())
		}
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := (&http.Client{Timeout: 500 * time.Millisecond}).Get("http://" + addr + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not become ready; output:\n%s", s.log.String())
	return nil
}

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

func uniqueTestISBN(t *testing.T) string {
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

func putBook(t *testing.T, baseURL, isbn string) {
	t.Helper()
	body := `{"title":"Persisted Across Restart","author":"Process Test","published_year":2024}`
	req, err := http.NewRequest(http.MethodPut, baseURL+"/books/"+isbn, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build put: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("put status = %d, want 200 (body %q)", resp.StatusCode, msg)
	}
}

func getBook(t *testing.T, baseURL, isbn string) map[string]any {
	t.Helper()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/books/" + isbn)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("get status = %d, want 200 (body %q)", resp.StatusCode, msg)
	}
	var book map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	return book
}

// TestProcessRestartPersistsBook is scenario 8 with a real process boundary:
// write through one server process, stop it, start a new process, and read the
// record back from the same database.
func TestProcessRestartPersistsBook(t *testing.T) {
	dsn := os.Getenv("BOOKAPI_TEST_DSN")
	if dsn == "" {
		t.Skip("BOOKAPI_TEST_DSN is not set; skipping Postgres integration test")
	}

	bin := filepath.Join(t.TempDir(), "bookapi")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build server: %v", err)
	}

	isbn := uniqueTestISBN(t)
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store for cleanup: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Delete(context.Background(), isbn); err != nil {
			t.Errorf("cleanup delete: %v", err)
		}
		if err := st.Close(); err != nil {
			t.Errorf("cleanup close: %v", err)
		}
	})

	addr := freeAddr(t)
	first := startServer(t, bin, dsn, addr)
	putBook(t, "http://"+addr, isbn)
	first.stop()

	startServer(t, bin, dsn, addr)
	got := getBook(t, "http://"+addr, isbn)
	if got["isbn"] != isbn {
		t.Errorf("isbn after restart = %v, want %s", got["isbn"], isbn)
	}
	if got["title"] != "Persisted Across Restart" {
		t.Errorf("title after restart = %v, want the written title", got["title"])
	}
}
