package phrases

import (
	"bufio"
)

// is determines if lookup intersects propert(ies)
func (lookup property) is(properties property) bool {
	return (lookup & properties) != 0
}

const (
	_AHLetter   = _ALetter | _HebrewLetter | _ExtendedPictographic // _ExtendedPictographic is added for phrases (vs words)
	_MidNumLetQ = _MidNumLet | _SingleQuote
	_Ignore     = _Extend | _Format | _ZWJ
)

// SplitFunc is a bufio.SplitFunc implementation of phrase segmentation, for use with bufio.Scanner.
var SplitFunc bufio.SplitFunc = splitFunc[[]byte]

// splitFunc is a bufio.SplitFunc implementation of phrase segmentation, for use with bufio.Scanner.
func splitFunc[T ~string | ~[]byte](data T, atEOF bool) (advance int, token T, err error) {
	var empty T
	if len(data) == 0 {
		return 0, empty, nil
	}

	// These vars are stateful across loop iterations
	var pos int
	var lastExIgnore property     // "last excluding ignored categories"
	var lastLastExIgnore property // "the last one before that"
	var regionalIndicatorCount int

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

	// https://unicode.org/reports/tr29/#WB1
	// Start of text always advances
	pos += w

	for {
		eot := pos == len(data) // "end of text"

		if eot {
			if !atEOF {
				// Token extends past current data, request more
				return 0, empty, nil
			}

			// https://unicode.org/reports/tr29/#WB2
			break
		}

		// Remember previous properties to avoid lookups/lookbacks
		last := current
		if !last.is(_Ignore) {
			lastLastExIgnore = lastExIgnore
			lastExIgnore = last
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
			break
		}

		// https://unicode.org/reports/tr29/#WB3
		if current.is(_LF) && last.is(_CR) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB3a
		// https://unicode.org/reports/tr29/#WB3b
		if (last | current).is(_Newline | _CR | _LF) {
			break
		}

		// https://unicode.org/reports/tr29/#WB3c
		if current.is(_ExtendedPictographic) && last.is(_ZWJ) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB3d
		if (current & last).is(_WSegSpace) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB4
		if current.is(_Extend | _Format | _ZWJ) {
			pos += w
			continue
		}

		// WB4 applies to subsequent rules; there is an implied "ignoring Extend & Format & ZWJ"
		// https://unicode.org/reports/tr29/#Grapheme_Cluster_and_Format_Rules
		// The previous/subsequent methods are shorthand for "seek a property but skip over Extend|Format|ZWJ on the way"

		// https://unicode.org/reports/tr29/#WB5
		if current.is(_AHLetter) && lastExIgnore.is(_AHLetter) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB6
		if current.is(_MidLetter|_MidNumLetQ) && lastExIgnore.is(_AHLetter) {
			advance, more := subsequent(_AHLetter, data[pos+w:], atEOF)

			if more {
				// Token extends past current data, request more
				return 0, empty, nil
			}

			if advance != notfound {
				pos += w + advance
				continue
			}
		}

		// https://unicode.org/reports/tr29/#WB7
		if current.is(_AHLetter) && lastExIgnore.is(_MidLetter|_MidNumLetQ) && lastLastExIgnore.is(_AHLetter) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB7a
		if current.is(_SingleQuote) && lastExIgnore.is(_HebrewLetter) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB7b
		if current.is(_DoubleQuote) && lastExIgnore.is(_HebrewLetter) {
			advance, more := subsequent(_HebrewLetter, data[pos+w:], atEOF)

			if more {
				// Token extends past current data, request more
				return 0, empty, nil
			}

			if advance != notfound {
				pos += w + advance
				continue
			}
		}

		// https://unicode.org/reports/tr29/#WB7c
		if current.is(_HebrewLetter) && lastExIgnore.is(_DoubleQuote) && lastLastExIgnore.is(_HebrewLetter) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB8
		// https://unicode.org/reports/tr29/#WB9
		// https://unicode.org/reports/tr29/#WB10
		// _WSegSpace is added for phrases: treat spaces adjacent to words as non-breaking.
		if current.is(_Numeric|_AHLetter|_WSegSpace) && lastExIgnore.is(_Numeric|_AHLetter|_WSegSpace) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB11
		if current.is(_Numeric) && lastExIgnore.is(_MidNum|_MidNumLetQ) && lastLastExIgnore.is(_Numeric) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB12
		if current.is(_MidNum|_MidNumLetQ) && lastExIgnore.is(_Numeric) {
			advance, more := subsequent(_Numeric, data[pos+w:], atEOF)

			if more {
				// Token extends past current data, request more
				return 0, empty, nil
			}

			if advance != notfound {
				pos += w + advance
				continue
			}
		}

		// https://unicode.org/reports/tr29/#WB13
		if current.is(_Katakana) && lastExIgnore.is(_Katakana) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB13a
		if current.is(_ExtendNumLet) && lastExIgnore.is(_AHLetter|_Numeric|_Katakana|_ExtendNumLet) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB13b
		if current.is(_AHLetter|_Numeric|_Katakana) && lastExIgnore.is(_ExtendNumLet) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB15 and
		// https://unicode.org/reports/tr29/#WB16
		if current.is(_RegionalIndicator) && lastExIgnore.is(_RegionalIndicator) {
			regionalIndicatorCount++

			odd := regionalIndicatorCount%2 == 1
			if odd {
				pos += w
				continue
			}
		}

		// https://unicode.org/reports/tr29/#WB999
		// If we fall through all the above rules, it's a phrase break
		break
	}

	return pos, data[:pos], nil
}
