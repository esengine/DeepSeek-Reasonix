package phrases

const notfound = -1

// subsequent looks ahead in the buffer until it hits a rune in properties,
// ignoring runes with the _Ignore property per WB4
func subsequent[T ~string | ~[]byte](properties property, data T, atEOF bool) (advance int, more bool) {
	i := 0
	for i < len(data) {
		lookup, w := lookup(data[i:])
		if w == 0 {
			if atEOF {
				// Nothing more to evaluate
				return notfound, false
			}
			// More to evaluate - return notfound to indicate no match found yet
			return notfound, true
		}

		if lookup.is(_Ignore) {
			i += w
			continue
		}

		if lookup.is(properties) {
			// Found it
			return i, false
		}

		// If we see a non-ignored character that doesn't match,
		// the property is definitely not "immediately subsequent"
		return notfound, false
	}

	// If we reach here, we've only seen ignored characters or incomplete runes
	if atEOF {
		// Nothing more to evaluate
		return notfound, false
	}

	// Need more - return notfound to indicate no match found yet
	return notfound, true
}
