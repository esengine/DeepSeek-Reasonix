package sentences

import (
	"bufio"
)

// is determines if lookup intersects propert(ies)
func (lookup property) is(properties property) bool {
	return (lookup & properties) != 0
}

const (
	_SATerm  = _STerm | _ATerm
	_ParaSep = _Sep | _CR | _LF
	_Ignore  = _Extend | _Format
)

// SplitFunc is a bufio.SplitFunc implementation of sentence segmentation, for use with bufio.Scanner.
//
// See https://unicode.org/reports/tr29/#Sentence_Boundaries.
var SplitFunc bufio.SplitFunc = splitFunc[[]byte]

// SplitFunc is a bufio.SplitFunc implementation of word segmentation, for use with bufio.Scanner.
func splitFunc[T ~string | ~[]byte](data T, atEOF bool) (advance int, token T, err error) {
	var empty T
	if len(data) == 0 {
		return 0, empty, nil
	}

	// These vars are stateful across loop iterations
	var pos int
	var lastExIgnore property     // "last excluding ignored categories"
	var lastLastExIgnore property // "last one before that"
	var lastExIgnoreSp property
	var lastExIgnoreClose property
	var lastExIgnoreSpClose property
	var lastExIgnoreParaSepSpClose property

	// Rules are usually of the form Cat1 × Cat2; "current" refers to the first property
	// to the right of the ×, from which we look back or forward

	current, w := lookup(data[pos:])
	if w == 0 {
		if !atEOF {
			// Rune extends past current data, request more
			return 0, empty, nil
		}
		pos = len(data)
		return pos, data[:pos], nil
	}

	// https://unicode.org/reports/tr29/#SB1
	// Start of text always advances
	pos += w

main:
	for {
		eot := pos == len(data) // "end of text"

		if eot {
			if !atEOF {
				// Token extends past current data, request more
				return 0, empty, nil
			}

			// https://unicode.org/reports/tr29/#SB2
			break
		}

		// Remember previous properties to avoid lookups/lookbacks
		last := current

		if !last.is(_Ignore) {
			lastLastExIgnore = lastExIgnore
			lastExIgnore = last
		}

		if !lastExIgnore.is(_Sp) {
			lastExIgnoreSp = lastExIgnore
		}

		if !lastExIgnore.is(_Close) {
			lastExIgnoreClose = lastExIgnore
		}

		if !lastExIgnoreSp.is(_Close) {
			lastExIgnoreSpClose = lastExIgnoreSp
		}

		if !lastExIgnoreSpClose.is(_ParaSep) {
			lastExIgnoreParaSepSpClose = lastExIgnoreSpClose
		}

		current, w = lookup(data[pos:])
		if w == 0 {
			if atEOF {
				// Just return the bytes, we can't do anything with them
				pos = len(data)
				break
			}
			// Rune extends past current data, request more
			return 0, empty, nil
		}

		// Optimization: no rule can possibly apply
		if current|last == 0 { // i.e. both are zero
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB3
		if current.is(_LF) && last.is(_CR) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB4
		if last.is(_ParaSep) {
			break
		}

		// https://unicode.org/reports/tr29/#SB5
		if current.is(_Extend | _Format) {
			pos += w
			continue
		}

		// SB5 applies to subsequent rules; there is an implied "ignoring Extend & Format"
		// https://unicode.org/reports/tr29/#Sentence_Boundary_Rules
		// The previous/subsequent methods are shorthand for "seek a property but skip over Extend & Format on the way"

		// https://unicode.org/reports/tr29/#SB6
		if current.is(_Numeric) && lastExIgnore.is(_ATerm) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB7
		if current.is(_Upper) && lastExIgnore.is(_ATerm) && lastLastExIgnore.is(_Upper|_Lower) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB8
		if lastExIgnoreSpClose.is(_ATerm) {
			p := pos

			// ( ¬(OLetter | Upper | Lower | ParaSep | SATerm) )*
			// Zero or more of not-the-above properties
			for p < len(data) {
				lookup, w := lookup(data[p:])
				if w == 0 {
					if atEOF {
						// Just return the bytes, we can't do anything with them
						pos = len(data)
						break main
					}
					// Rune extends past current data, request more
					return 0, empty, nil
				}

				if lookup.is(_OLetter | _Upper | _Lower | _ParaSep | _SATerm) {
					break
				}

				p += w
			}

			advance, more := subsequent(_Lower, data[p:], atEOF)

			if more {
				// Rune or token extends past current data, request more
				return 0, empty, nil
			}

			if advance != notfound {
				pos = p + advance
				continue
			}
		}

		// https://unicode.org/reports/tr29/#SB8a
		if current.is(_SContinue|_SATerm) && lastExIgnoreSpClose.is(_SATerm) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB9
		if current.is(_Close|_Sp|_ParaSep) && lastExIgnoreClose.is(_SATerm) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB10
		if current.is(_Sp|_ParaSep) && lastExIgnoreSpClose.is(_SATerm) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#SB11
		if lastExIgnore.is(_SATerm) ||
			(lastExIgnore.is(_Close) && lastExIgnoreClose.is(_SATerm)) ||
			(lastExIgnore.is(_Sp) && lastExIgnoreSpClose.is(_SATerm)) ||
			(lastExIgnore.is(_ParaSep) && lastExIgnoreParaSepSpClose.is(_SATerm)) {
			break
		}

		// https://unicode.org/reports/tr29/#SB998
		pos += w
	}

	// Return token
	return pos, data[:pos], nil
}
