package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"example.com/bookapi/book"
)

func uniqueAuthor(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("list-author-%d-%d", time.Now().UnixNano(), os.Getpid())
}

func TestListOrderedAndFiltered(t *testing.T) {
	dsn := testDSN(t)
	s := testStore(t)
	author := uniqueAuthor(t)
	other := uniqueAuthor(t)
	isbns := []string{uniqueISBN(t), uniqueISBN(t)}
	sort.Strings(isbns)
	low, high := isbns[0], isbns[1]
	otherISBN := uniqueISBN(t)

	for _, isbn := range []string{low, high, otherISBN} {
		isbn := isbn
		t.Cleanup(func() { deleteBook(t, dsn, isbn) })
	}

	// Store the higher ISBN first so ordering cannot come from insertion order.
	for _, b := range []book.Book{
		{ISBN: high, Title: "High", Author: author, PublishedYear: 2002},
		{ISBN: low, Title: "Low", Author: author, PublishedYear: 2001},
		{ISBN: otherISBN, Title: "Other", Author: other, PublishedYear: 2003},
	} {
		if err := s.Put(context.Background(), b); err != nil {
			t.Fatalf("put %s: %v", b.ISBN, err)
		}
	}

	got, err := s.List(context.Background(), &author)
	if err != nil {
		t.Fatalf("list by author: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list by author = %+v, want exactly the two books for %q", got, author)
	}
	if got[0].ISBN != low || got[1].ISBN != high {
		t.Errorf("list by author = [%s, %s], want [%s, %s] ordered by isbn", got[0].ISBN, got[1].ISBN, low, high)
	}

	got, err = s.List(context.Background(), &other)
	if err != nil {
		t.Fatalf("list by other author: %v", err)
	}
	if len(got) != 1 || got[0].ISBN != otherISBN {
		t.Errorf("list by other author = %+v, want only %s", got, otherISBN)
	}

	all, err := s.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	positions := map[string]int{}
	for i, b := range all {
		positions[b.ISBN] = i
	}
	lowPos, okLow := positions[low]
	highPos, okHigh := positions[high]
	if !okLow || !okHigh {
		t.Fatalf("list all did not contain both test books: %+v", positions)
	}
	if lowPos >= highPos {
		t.Errorf("list all order: %s at %d, %s at %d; want ascending isbn", low, lowPos, high, highPos)
	}
}

func TestListNoMatchesIsEmptyNotNil(t *testing.T) {
	s := testStore(t)
	missing := uniqueAuthor(t)

	got, err := s.List(context.Background(), &missing)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got == nil {
		t.Fatal("list returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("list = %+v, want empty", got)
	}
}
