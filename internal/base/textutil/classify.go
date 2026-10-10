package textutil

import "unicode"

// The three character classes every filter in this package is built from, so
// a rune is judged the same way wherever it is met.
func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) }

func isBidi(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

func isFormat(r rune) bool { return unicode.Is(unicode.Cf, r) }
