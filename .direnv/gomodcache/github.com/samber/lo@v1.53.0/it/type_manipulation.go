//go:build go1.23

package it

import (
	"iter"

	"github.com/samber/lo"
)

// ToSeqPtr returns a sequence of pointers to each value.
// Play: https://go.dev/play/p/Z9nA8cC3dXw
func ToSeqPtr[T any](collection iter.Seq[T]) iter.Seq[*T] {
	return Map(collection, lo.ToPtr)
}

// FromSeqPtr returns a sequence with the pointer values.
// Returns a zero value in case of a nil pointer element.
// Play: https://go.dev/play/p/A1bB9dD4eYx
func FromSeqPtr[T any](collection iter.Seq[*T]) iter.Seq[T] {
	return Map(collection, lo.FromPtr)
}

// FromSeqPtrOr returns a sequence with the pointer values or the fallback value.
// Play: https://go.dev/play/p/B2cC8eE5fYz
func FromSeqPtrOr[T any](collection iter.Seq[*T], fallback T) iter.Seq[T] {
	return Map(collection, func(x *T) T { return lo.FromPtrOr(x, fallback) })
}

// ToAnySeq returns a sequence with all elements mapped to `any` type.
// Play: https://go.dev/play/p/C3dD9fF6Za1
func ToAnySeq[T any](collection iter.Seq[T]) iter.Seq[any] {
	return Map(collection, func(x T) any { return x })
}

// FromAnySeq returns a sequence with all elements mapped to a type.
// Panics on type conversion failure.
// Play: https://go.dev/play/p/D4eE0gG7Ab2
func FromAnySeq[T any](collection iter.Seq[any]) iter.Seq[T] {
	return Map(collection, func(item any) T {
		if t, ok := item.(T); ok {
			return t
		}
		panic("it.FromAnySeq: type conversion failed")
	})
}

// Empty returns an empty sequence.
// Play: https://go.dev/play/p/E5fF1hH8Bc3
func Empty[T any]() iter.Seq[T] {
	return func(yield func(T) bool) {}
}

// IsEmpty returns true if the sequence is empty.
// Will iterate at most once.
// Play: https://go.dev/play/p/F6gG2iI9Cd4
func IsEmpty[T any](collection iter.Seq[T]) bool {
	for range collection {
		return false
	}
	return true
}

// IsNotEmpty returns true if the sequence is not empty.
// Will iterate at most once.
// Play: https://go.dev/play/p/G7hH3jJ0De5
func IsNotEmpty[T any](collection iter.Seq[T]) bool {
	return !IsEmpty(collection)
}

// CoalesceSeq returns the first non-empty sequence.
// Will iterate through each sub-sequence at most once.
// Play: https://go.dev/play/p/H8iI4kK1Ef6
func CoalesceSeq[T any](v ...iter.Seq[T]) (iter.Seq[T], bool) {
	for i := range v {
		for range v[i] {
			return v[i], true
		}
	}
	return Empty[T](), false
}

// CoalesceSeqOrEmpty returns the first non-empty sequence.
// Will iterate through each sub-sequence at most once.
// Play: https://go.dev/play/p/I9jJ5lL2Fg7
func CoalesceSeqOrEmpty[T any](v ...iter.Seq[T]) iter.Seq[T] {
	result, _ := CoalesceSeq(v...)
	return result
}
