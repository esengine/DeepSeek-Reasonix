package capdiag

import "unicode/utf8"

// runeBoundary returns the largest index at or below n that starts a rune in
// s, so a byte cap never splits a multi-byte character. Errors and stderr are
// often localized, multi-byte text.
func runeBoundary(s string, n int) int {
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}
