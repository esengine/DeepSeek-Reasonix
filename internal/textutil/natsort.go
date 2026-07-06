package textutil

import (
	"strconv"
	"strings"
	"unicode"
)

// NaturalLess compares two strings using natural sort order:
// numeric substrings are compared as numbers, and the rest is
// compared case-insensitively. This produces the ordering humans
// expect: "file2" < "file10", "第1章" < "第10章".
//
// When a digit run and a non-digit run meet, digits sort first
// (matching the behaviour of Windows File Explorer and macOS Finder).
func NaturalLess(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	ra := []rune(a)
	rb := []rune(b)

	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		aDig := unicode.IsDigit(ra[i])
		bDig := unicode.IsDigit(rb[j])

		if aDig && bDig {
			// Both are digit runs: extract and compare numerically.
			aStart := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			bStart := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}

			aNum, _ := strconv.Atoi(string(ra[aStart:i]))
			bNum, _ := strconv.Atoi(string(rb[bStart:j]))
			if aNum != bNum {
				return aNum < bNum
			}
			// Same numeric value: shorter digit string first (e.g. "1" < "01").
			aLen := i - aStart
			bLen := j - bStart
			if aLen != bLen {
				return aLen < bLen
			}
		} else if !aDig && !bDig {
			// Both non-digits: compare runes.
			if ra[i] != rb[j] {
				return ra[i] < rb[j]
			}
			i++
			j++
		} else {
			// Mixed digit / non-digit: digit comes first.
			return aDig
		}
	}
	return len(ra) < len(rb)
}
