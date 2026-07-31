package words

// Iterator is a generic iterator for words in strings or byte slices,
// with an ASCII hot path optimization.
type Iterator[T ~string | ~[]byte] struct {
	split func(T, bool) (int, T, error)
	data  T
	pos   int
	start int
}

var (
	splitFuncString = splitFunc[string]
	splitFuncBytes  = splitFunc[[]byte]
)

// FromString returns an iterator for the words in the input string.
// Iterate while Next() is true, and access the word via Value().
func FromString(s string) *Iterator[string] {
	return &Iterator[string]{
		split: splitFuncString,
		data:  s,
	}
}

// FromBytes returns an iterator for the words in the input bytes.
// Iterate while Next() is true, and access the word via Value().
func FromBytes(b []byte) *Iterator[[]byte] {
	return &Iterator[[]byte]{
		split: splitFuncBytes,
		data:  b,
	}
}

// isASCIIAlphanumeric returns true if b is in [a-zA-Z0-9]
func isASCIIAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// Next advances the iterator to the next word.
// Returns false when there are no more words.
func (iter *Iterator[T]) Next() bool {
	if iter.pos >= len(iter.data) {
		return false
	}
	iter.start = iter.pos

	// ASCII hot path: consume contiguous ASCII alphanumerics
	// followed by ASCII space (or end of data)
	b := iter.data[iter.pos]
	if isASCIIAlphanumeric(b) {
		// Consume all contiguous ASCII alphanumerics
		end := iter.pos + 1
		for end < len(iter.data) && isASCIIAlphanumeric(iter.data[end]) {
			end++
		}
		// Check if followed by ASCII space or end of data
		if end >= len(iter.data) || iter.data[end] == ' ' {
			iter.pos = end
			return true
		}
		// Otherwise fall through to splitFunc
	}

	// Fall back to full word parsing
	remaining := iter.data[iter.pos:]
	advance, _, err := iter.split(remaining, true)
	if err != nil {
		panic(err)
	}
	if advance <= 0 {
		panic("splitFunc returned a zero or negative advance")
	}
	iter.pos += advance
	if iter.pos > len(iter.data) {
		panic("splitFunc advanced beyond end of data")
	}
	return true
}

// Value returns the current word.
func (iter *Iterator[T]) Value() T {
	return iter.data[iter.start:iter.pos]
}

// Start returns the byte position of the current word in the original data.
func (iter *Iterator[T]) Start() int {
	return iter.start
}

// End returns the byte position after the current word in the original data.
func (iter *Iterator[T]) End() int {
	return iter.pos
}

// Reset resets the iterator to the beginning of the data.
func (iter *Iterator[T]) Reset() {
	iter.start = 0
	iter.pos = 0
}

// SetText sets the data for the iterator to operate on, and resets all state.
func (iter *Iterator[T]) SetText(data T) {
	iter.data = data
	iter.start = 0
	iter.pos = 0
}

// Split sets the SplitFunc for the Iterator.
func (iter *Iterator[T]) Split(split func(T, bool) (int, T, error)) {
	iter.split = split
}

// First returns the first word without advancing the iterator.
func (iter *Iterator[T]) First() T {
	if len(iter.data) == 0 {
		return iter.data
	}

	// Use a copy to leverage Next()'s ASCII optimization
	cp := *iter
	cp.pos = 0
	cp.start = 0
	cp.Next()
	return cp.Value()
}
