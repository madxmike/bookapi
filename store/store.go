// Package store persists books in Postgres.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	"example.com/bookapi/book"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned by Get when no book has the requested ISBN.
var ErrNotFound = errors.New("book not found")

// Store is a handle to the books table.
type Store struct {
	db *sql.DB
}

// Open opens the database at dsn and verifies it is reachable.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Put inserts the book, replacing any existing row with the same ISBN.
func (s *Store) Put(ctx context.Context, b book.Book) error {
	const query = `INSERT INTO books (isbn, title, author, published_year) VALUES ($1,$2,$3,$4) ON CONFLICT (isbn) DO UPDATE SET title=EXCLUDED.title, author=EXCLUDED.author, published_year=EXCLUDED.published_year, updated_at=now()`
	if _, err := s.db.ExecContext(ctx, query, b.ISBN, b.Title, b.Author, b.PublishedYear); err != nil {
		return fmt.Errorf("put book %s: %w", b.ISBN, err)
	}
	return nil
}

// Get returns the book with the given ISBN, or ErrNotFound if there is none.
func (s *Store) Get(ctx context.Context, isbn string) (book.Book, error) {
	const query = `SELECT isbn, title, author, published_year FROM books WHERE isbn = $1`
	var b book.Book
	err := s.db.QueryRowContext(ctx, query, isbn).Scan(&b.ISBN, &b.Title, &b.Author, &b.PublishedYear)
	if errors.Is(err, sql.ErrNoRows) {
		return book.Book{}, ErrNotFound
	}
	if err != nil {
		return book.Book{}, fmt.Errorf("get book %s: %w", isbn, err)
	}
	return b, nil
}

// Delete removes the book with the given canonical ISBN.
// Deleting an absent row is not an error.
func (s *Store) Delete(ctx context.Context, isbn string) error {
	const query = `DELETE FROM books WHERE isbn = $1`
	if _, err := s.db.ExecContext(ctx, query, isbn); err != nil {
		return fmt.Errorf("delete book %s: %w", isbn, err)
	}
	return nil
}

// List returns the books ordered by ISBN. When author is non-nil, only books
// whose author is an exact match are returned. The result is never nil.
func (s *Store) List(ctx context.Context, author *string) ([]book.Book, error) {
	query := `SELECT isbn, title, author, published_year FROM books`
	var args []any
	if author != nil {
		query += ` WHERE author = $1`
		args = append(args, *author)
	}
	query += ` ORDER BY isbn ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	books := []book.Book{}
	for rows.Next() {
		var b book.Book
		if err := rows.Scan(&b.ISBN, &b.Title, &b.Author, &b.PublishedYear); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	return books, nil
}

// Count returns the number of rows stored under isbn, which is at most one.
func (s *Store) Count(ctx context.Context, isbn string) (int, error) {
	const query = `SELECT count(*) FROM books WHERE isbn = $1`
	var n int
	if err := s.db.QueryRowContext(ctx, query, isbn).Scan(&n); err != nil {
		return 0, fmt.Errorf("count books %s: %w", isbn, err)
	}
	return n, nil
}

// Migrate applies the embedded schema files in name order.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		schema, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(schema)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}
