package sentences

// FromString returns an iterator for the sentences in the input string.
// Iterate while Next() is true, and access the sentence via Value().
func FromString(s string) *Iterator[string] {
	return &Iterator[string]{
		split: splitFuncString,
		data:  s,
	}
}

// FromBytes returns an iterator for the sentences in the input bytes.
// Iterate while Next() is true, and access the sentence via Value().
func FromBytes(b []byte) *Iterator[[]byte] {
	return &Iterator[[]byte]{
		split: splitFuncBytes,
		data:  b,
	}
}

// Iterator is a generic iterator for sentences in strings or byte slices.
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

// isASCIIAlphanumericOrSpace returns true if b is in [a-zA-Z0-9 ]
func isASCIIAlphanumericOrSpace(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == ' '
}

// Next advances the iterator to the next sentence.
// Returns false when there are no more sentences.
func (iter *Iterator[T]) Next() bool {
	if iter.pos >= len(iter.data) {
		return false
	}
	iter.start = iter.pos

	// ASCII hot path: skip contiguous ASCII alphanumerics and spaces
	// These characters never trigger sentence breaks by themselves
	for iter.pos < len(iter.data) && isASCIIAlphanumericOrSpace(iter.data[iter.pos]) {
		iter.pos++
	}

	// If we consumed all remaining data, we're done
	if iter.pos >= len(iter.data) {
		return true
	}

	// If we skipped any ASCII, back up one so splitfunc has "last" context
	if iter.pos > iter.start {
		iter.pos--
	}

	// Defer to splitfunc for the rest
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

// Value returns the current sentence.
func (iter *Iterator[T]) Value() T {
	return iter.data[iter.start:iter.pos]
}

// Start returns the byte position of the current sentence in the original data.
func (iter *Iterator[T]) Start() int {
	return iter.start
}

// End returns the byte position after the current sentence in the original data.
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

// First returns the first sentence without advancing the iterator.
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
