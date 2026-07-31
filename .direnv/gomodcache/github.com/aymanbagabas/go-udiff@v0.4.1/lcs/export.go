package lcs

import "strings"

// DiffStrings returns the differences between two strings.
// It does not respect rune boundaries.
func DiffStrings(a, b string) []Diff {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	return diff(linesSeqs{aLines, bLines})
}
