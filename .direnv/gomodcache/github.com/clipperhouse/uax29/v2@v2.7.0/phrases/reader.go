// Package phrases implements Unicode phrase boundaries: https://unicode.org/reports/tr29/#phrase_Boundaries
package phrases

import (
	"bufio"
	"io"
)

type Scanner struct {
	*bufio.Scanner
}

// FromReader returns a Scanner, to split phrases. "Phrase" is defined as
// a series of words separated only by spaces.
//
// It embeds a [bufio.Scanner], so you can use its methods.
//
// Iterate through phrases by calling Scan() until false, then check Err().
func FromReader(r io.Reader) *Scanner {
	sc := bufio.NewScanner(r)
	sc.Split(SplitFunc)
	return &Scanner{
		Scanner: sc,
	}
}
