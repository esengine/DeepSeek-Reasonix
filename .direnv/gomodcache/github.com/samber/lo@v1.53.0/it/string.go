//go:build go1.23

package it

import "iter"

// ChunkString returns a sequence of strings split into groups of length size. If the string can't be split evenly,
// the final chunk will be the remaining characters.
// Play: https://go.dev/play/p/Y4mN8bB2cXw
//
// Note: it.ChunkString and it.Chunk functions behave inconsistently for empty input: it.ChunkString("", n) returns [""] instead of [].
// See https://github.com/samber/lo/issues/788
func ChunkString[T ~string](str T, size int) iter.Seq[T] {
	if size <= 0 {
		panic("it.ChunkString: size must be greater than 0")
	}

	return func(yield func(T) bool) {
		if len(str) == 0 || size >= len(str) {
			yield(str)
			return
		}

		currentLen := 0
		currentStart := 0
		for i := range str {
			if currentLen == size {
				if !yield(str[currentStart:i]) {
					return
				}
				currentLen = 0
				currentStart = i
			}
			currentLen++
		}
		yield(str[currentStart:])
	}
}
