// Package book defines the domain type shared by storage and transport.
package book

// Book is one catalog record, keyed by its canonical ISBN-13.
type Book struct {
	ISBN          string
	Title         string
	Author        string
	PublishedYear int
}
