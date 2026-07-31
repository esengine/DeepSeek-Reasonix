package words

// BleveNumeric determines if a token is Numeric using the Bleve segmenter's.
// definition, see: https://github.com/blevesearch/segment/blob/master/segment_words.rl#L199-L207
// This API is experimental.
func BleveNumeric(token []byte) bool {
	var pos, w int
	var current property
	var lastExIgnore property     // "last excluding ignored categories"
	var lastLastExIgnore property // "the last one before that"

	for pos < len(token) {
		// Remember previous properties to avoid lookups/lookbacks
		last := current
		if !last.is(_Ignore) {
			lastLastExIgnore = lastExIgnore
			lastExIgnore = last
		}

		current, w = lookup(token[pos:])

		if pos == 0 {
			// must start with Numeric|ExtendNumLet
			if current.is(_Numeric | _ExtendNumLet) {
				pos += w
				continue
			}
			// not numeric, can move on
			return false
		}

		// https://unicode.org/reports/tr29/#WB8
		if last.is(_Numeric) && current.is(_Numeric) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB11
		if current.is(_Numeric) && lastExIgnore.is(_MidNum|_MidNumLetQ) && lastLastExIgnore.is(_Numeric) {
			pos += w
			continue
		}

		// WB12.  Numeric × (MidNum | MidNumLet | Single_Quote) Numeric
		// https://unicode.org/reports/tr29/#WB12
		if current.is(_MidNum|_MidNumLetQ) && lastExIgnore.is(_Numeric) {
			advance, _ := subsequent(_Numeric, token[pos+w:], true)
			if advance != notfound {
				pos += w + advance
				continue
			}
		}

		// https://unicode.org/reports/tr29/#WB13a
		if current.is(_ExtendNumLet) && lastExIgnore.is(_Numeric|_ExtendNumLet) {
			pos += w
			continue
		}

		// https://unicode.org/reports/tr29/#WB13b
		if current.is(_Numeric) && lastExIgnore.is(_ExtendNumLet) {
			pos += w
			continue
		}

		// if we get here, none of the above rules apply
		return false
	}

	return true
}

// BleveIdeographic determines if a token is comprised ideographs, by the
// Bleve segmenter's definition. It is the union of Han, Katakana, & Hiragana.
// See https://github.com/blevesearch/segment/blob/master/segment_words.rl
// ...and search for uses of "Ideo". This API is experimental.
func BleveIdeographic(token []byte) bool {
	var pos int

	for pos < len(token) {
		current, w := lookup(token[pos:])

		if pos == 0 {
			// must start with ideo
			if current.is(_BleveIdeographic) {
				pos += w
				continue
			}
			// not ideo, can move on
			return false
		}

		// approximates https://unicode.org/reports/tr29/#WB13
		if current.is(_BleveIdeographic | _ExtendNumLet | _Ignore) {
			pos += w
			continue
		}

		// if we get here, none of the above rules apply
		return false
	}

	return true
}
