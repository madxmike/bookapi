package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"example.com/bookapi/book"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("BOOKAPI_TEST_DSN")
	if dsn == "" {
		t.Skip("BOOKAPI_TEST_DSN is not set; skipping Postgres integration test")
	}
	return dsn
}

func openStore(t *testing.T, dsn string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

func testStore(t *testing.T) *Store {
	t.Helper()
	s := openStore(t, testDSN(t))
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// uniqueISBN returns a valid ISBN-13 that does not collide with other tests.
func uniqueISBN(t *testing.T) string {
	t.Helper()
	n := (time.Now().UnixNano() ^ int64(os.Getpid())) % 1_000_000_000
	first12 := fmt.Sprintf("978%09d", n)
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

func deleteBook(t *testing.T, dsn, isbn string) {
	t.Helper()
	s := openStore(t, dsn)
	if err := s.Delete(context.Background(), isbn); err != nil {
		t.Fatalf("delete book %s: %v", isbn, err)
	}
}

func countRows(t *testing.T, s *Store, isbn string) int {
	t.Helper()
	n, err := s.Count(context.Background(), isbn)
	if err != nil {
		t.Fatalf("count rows for %s: %v", isbn, err)
	}
	return n
}

func TestPutGetRoundTrip(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	want := book.Book{
		ISBN:          isbn,
		Title:         "The Go Programming Language",
		Author:        "Alan Donovan and Brian Kernighan",
		PublishedYear: 2015,
	}
	if err := s.Put(context.Background(), want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != want {
		t.Errorf("get = %+v, want %+v", got, want)
	}
}

func TestPutIdenticalTwiceKeepsOneRow(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	want := book.Book{ISBN: isbn, Title: "Same Title", Author: "Same Author", PublishedYear: 2001}
	for i := 0; i < 2; i++ {
		if err := s.Put(context.Background(), want); err != nil {
			t.Fatalf("put %d: %v", i+1, err)
		}
	}

	got, err := s.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != want {
		t.Errorf("get = %+v, want %+v", got, want)
	}
	if n := countRows(t, s, isbn); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestPutTwiceLastWriteWins(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	first := book.Book{ISBN: isbn, Title: "First Title", Author: "First Author", PublishedYear: 2001}
	second := book.Book{ISBN: isbn, Title: "Second Title", Author: "Second Author", PublishedYear: 2002}
	if err := s.Put(context.Background(), first); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := s.Put(context.Background(), second); err != nil {
		t.Fatalf("second put: %v", err)
	}

	got, err := s.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != second {
		t.Errorf("get = %+v, want %+v", got, second)
	}
	if n := countRows(t, s, isbn); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestConcurrentPutsKeepOneRow(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	const writers = 32
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := book.Book{
				ISBN:          isbn,
				Title:         fmt.Sprintf("title-%d", i),
				Author:        fmt.Sprintf("author-%d", i),
				PublishedYear: 2000 + i,
			}
			if err := s.Put(context.Background(), b); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent put: %v", err)
	}

	if n := countRows(t, s, isbn); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	if _, err := s.Get(context.Background(), isbn); err != nil {
		t.Errorf("get after concurrent puts: %v", err)
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	s := testStore(t)

	_, err := s.Get(context.Background(), uniqueISBN(t))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v, want %v", err, ErrNotFound)
	}
}

func TestDeleteRemovesRow(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	b := book.Book{ISBN: isbn, Title: "Deleted Title", Author: "Deleted Author", PublishedYear: 2003}
	if err := s.Put(context.Background(), b); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Delete(context.Background(), isbn); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := s.Get(context.Background(), isbn); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete = %v, want %v", err, ErrNotFound)
	}
	if n := countRows(t, s, isbn); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestDeleteAbsentReturnsNil(t *testing.T) {
	s := testStore(t)
	isbn := uniqueISBN(t)

	if err := s.Delete(context.Background(), isbn); err != nil {
		t.Errorf("delete absent: %v", err)
	}
	if n := countRows(t, s, isbn); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestDeleteTwiceReturnsNil(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	b := book.Book{ISBN: isbn, Title: "Twice Title", Author: "Twice Author", PublishedYear: 2004}
	if err := s.Put(context.Background(), b); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Delete(context.Background(), isbn); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := s.Delete(context.Background(), isbn); err != nil {
		t.Errorf("second delete: %v", err)
	}
	if n := countRows(t, s, isbn); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestConcurrentDeletesRemoveRow(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	b := book.Book{ISBN: isbn, Title: "Concurrent Title", Author: "Concurrent Author", PublishedYear: 2005}
	if err := s.Put(context.Background(), b); err != nil {
		t.Fatalf("put: %v", err)
	}

	const deleters = 16
	var wg sync.WaitGroup
	errs := make(chan error, deleters)
	for i := 0; i < deleters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Delete(context.Background(), isbn); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent delete: %v", err)
	}

	if n := countRows(t, s, isbn); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if _, err := s.Get(context.Background(), isbn); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after concurrent deletes = %v, want %v", err, ErrNotFound)
	}
}

func TestTwoStoresSeeEachOthersWrites(t *testing.T) {
	dsn := testDSN(t)
	first := testStore(t)
	second := openStore(t, dsn)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	want := book.Book{ISBN: isbn, Title: "Shared Title", Author: "Shared Author", PublishedYear: 2020}
	if err := first.Put(context.Background(), want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := second.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get from second store: %v", err)
	}
	if got != want {
		t.Errorf("get from second store = %+v, want %+v", got, want)
	}
}

func TestCloseAndReopenStillReads(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	isbn := uniqueISBN(t)
	t.Cleanup(func() { deleteBook(t, dsn, isbn) })

	want := book.Book{ISBN: isbn, Title: "Survives Restart", Author: "Persistent Author", PublishedYear: 1999}
	if err := s.Put(context.Background(), want); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened := openStore(t, dsn)
	got, err := reopened.Get(context.Background(), isbn)
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got != want {
		t.Errorf("get after reopen = %+v, want %+v", got, want)
	}
}
