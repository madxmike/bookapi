package isbn

import (
	"errors"
	"testing"
)

func TestCanonical(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "isbn13 with hyphens", input: "978-0-13-110362-7", want: "9780131103627"},
		{name: "isbn10 with hyphens", input: "0-13-110362-8", want: "9780131103627"},
		{name: "isbn10 plain", input: "0306406152", want: "9780306406157"},
		{name: "isbn10 with spaces", input: "0 13 110362 8", want: "9780131103627"},
		{name: "isbn10 with mixed separators", input: "0-13 1103-62-8", want: "9780131103627"},
		{name: "isbn10 X check digit", input: "080442957X", want: "9780804429573"},
		{name: "isbn10 lowercase x check digit", input: "080442957x", want: "9780804429573"},
		{name: "isbn10 X with hyphens", input: "0-8044-2957-X", want: "9780804429573"},
		{name: "isbn13 plain", input: "9780131103627", want: "9780131103627"},
		{name: "isbn13 trailing space", input: " 9780131103627 ", want: "9780131103627"},
		{name: "isbn13 with repeated separators", input: "978--0-13  110362-7", want: "9780131103627"},
		{name: "isbn13 979 prefix", input: "9791034302871", want: "9791034302871"},
		{name: "isbn13 979 with hyphens", input: "979-10-343-0287-1", want: "9791034302871"},
		{name: "isbn13 bad checksum", input: "9780131103628", wantErr: true},
		{name: "isbn13 bad checksum 979", input: "9791034302870", wantErr: true},
		{name: "twelve digits", input: "978013110362", wantErr: true},
		{name: "977 prefix with valid checksum", input: "9770131103626", wantErr: true},
		{name: "977 prefix literal from ledger", input: "9770131103627", wantErr: true},
		{name: "isbn10 bad checksum", input: "0306406153", wantErr: true},
		{name: "isbn10 bad X placement", input: "08044295X3", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "only separators", input: "- - ", wantErr: true},
		{name: "short", input: "12345", wantErr: true},
		{name: "long", input: "97801311036270", wantErr: true},
		{name: "non digit isbn13", input: "9780131A03627", wantErr: true},
		{name: "non digit isbn10", input: "03064061S2", wantErr: true},
		{name: "tab is not ignored", input: "978\t0131103627", wantErr: true},
		{name: "isbn13 X check digit", input: "978013110362X", wantErr: true},
		{name: "isbn10 too short after separators", input: "0-13-110362", wantErr: true},
		{name: "isbn10 leading zero preserved", input: "0131103628", want: "9780131103627"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonical(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("Canonical(%q) error = %v, want ErrInvalid", tt.input, err)
				}
				if got != "" {
					t.Fatalf("Canonical(%q) = %q on error, want empty string", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Canonical(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("Canonical(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func FuzzCanonicalIdempotent(f *testing.F) {
	f.Add("978-0-13-110362-7")
	f.Add("0-13-110362-8")
	f.Add("0306406152")
	f.Add("080442957X")
	f.Add("9791034302871")
	f.Add("9770131103626")
	f.Add("9780131103628")
	f.Add("")
	f.Add("not an isbn")
	f.Add("978013110362")

	f.Fuzz(func(t *testing.T, s string) {
		first, err := Canonical(s)
		if err != nil {
			return
		}
		second, err := Canonical(first)
		if err != nil {
			t.Fatalf("Canonical(%q) = %q, but Canonical(%q) failed: %v", s, first, first, err)
		}
		if second != first {
			t.Fatalf("not idempotent: Canonical(%q) = %q, then Canonical(%q) = %q", s, first, first, second)
		}
	})
}
