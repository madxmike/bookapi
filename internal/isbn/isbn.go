// Package isbn validates ISBN-10 and ISBN-13 values and normalizes them to
// canonical ISBN-13 form.
package isbn

import "errors"

// ErrInvalid is returned for any input that is not a valid ISBN.
var ErrInvalid = errors.New("invalid isbn")

// Canonical returns the canonical ISBN-13 for s, ignoring hyphens and spaces.
func Canonical(s string) (string, error) {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '-' && s[i] != ' ' {
			b = append(b, s[i])
		}
	}

	switch len(b) {
	case 10:
		for i := 0; i < 9; i++ {
			if b[i] < '0' || b[i] > '9' {
				return "", ErrInvalid
			}
		}

		var check int
		switch last := b[9]; {
		case last == 'X' || last == 'x':
			check = 10
		case last >= '0' && last <= '9':
			check = int(last - '0')
		default:
			return "", ErrInvalid
		}

		sum := 10 * check
		for i := 0; i < 9; i++ {
			sum += (i + 1) * int(b[i]-'0')
		}
		if sum%11 != 0 {
			return "", ErrInvalid
		}

		out := make([]byte, 13)
		copy(out, "978")
		copy(out[3:], b[:9])
		out[12] = byte('0' + checkDigit13(out[:12]))
		return string(out), nil

	case 13:
		for i := 0; i < 13; i++ {
			if b[i] < '0' || b[i] > '9' {
				return "", ErrInvalid
			}
		}
		prefix := string(b[:3])
		if prefix != "978" && prefix != "979" {
			return "", ErrInvalid
		}
		if int(b[12]-'0') != checkDigit13(b[:12]) {
			return "", ErrInvalid
		}
		return string(b), nil

	default:
		return "", ErrInvalid
	}
}

// checkDigit13 computes the ISBN-13 check digit for the first 12 digits.
func checkDigit13(first12 []byte) int {
	sum := 0
	for i, c := range first12 {
		d := int(c - '0')
		if i%2 == 0 {
			sum += d
		} else {
			sum += 3 * d
		}
	}
	return (10 - sum%10) % 10
}
