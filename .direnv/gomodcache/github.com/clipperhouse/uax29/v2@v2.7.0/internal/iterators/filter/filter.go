package filter

import (
	"unicode"
	"unicode/utf8"
)

type Func func([]byte) bool

// AlphaNumeric is a filter which returns only tokens
// that contain a Letter or Number, as defined by Unicode.
var AlphaNumeric Func = func(token []byte) bool {
	pos := 0
	for pos < len(token) {
		r, w := utf8.DecodeRune(token[pos:])
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			// we use these methods instead of unicode.In for
			// performance; these methods have ASCII fast paths
			return true
		}
		pos += w
	}

	return false
}
