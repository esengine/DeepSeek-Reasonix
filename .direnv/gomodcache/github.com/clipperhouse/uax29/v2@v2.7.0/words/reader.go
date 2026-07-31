// Package words implements Unicode word boundaries: https://unicode.org/reports/tr29/#Word_Boundaries
package words

import (
	"bufio"
	"io"
)

type Scanner struct {
	*bufio.Scanner
}

// FromReader returns a Scanner, to split words per
// https://unicode.org/reports/tr29/#Word_Boundaries.
//
// It embeds a [bufio.Scanner], so you can use its methods.
//
// Iterate through words by calling Scan() until false, then check Err().
func FromReader(r io.Reader) *Scanner {
	s := bufio.NewScanner(r)
	s.Split(SplitFunc)
	sc := &Scanner{
		Scanner: s,
	}
	return sc
}

// Joiners sets runes that should be treated like word characters, where
// otherwsie words sill be split. See the [Joiners] type.
func (sc *Scanner) Joiners(j *Joiners[[]byte]) {
	sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		return j.splitFunc(data, atEOF)
	})
}
