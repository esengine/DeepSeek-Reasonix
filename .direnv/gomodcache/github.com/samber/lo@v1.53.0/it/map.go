//go:build go1.23

package it

import (
	"iter"
	"maps"
)

// Keys creates a sequence of the map keys.
// Play: https://go.dev/play/p/Fu7h-eW18QM
func Keys[K comparable, V any](in ...map[K]V) iter.Seq[K] {
	return func(yield func(K) bool) {
		for i := range in {
			for k := range in[i] {
				if !yield(k) {
					return
				}
			}
		}
	}
}

// UniqKeys creates a sequence of unique keys in the map.
// Will allocate a map large enough to hold all distinct input keys.
// Long input sequences with heterogeneous keys can cause excessive memory usage.
// Play: https://go.dev/play/p/_NicwfgAHbO
func UniqKeys[K comparable, V any](in ...map[K]V) iter.Seq[K] {
	return func(yield func(K) bool) {
		seen := make(map[K]struct{})

		for i := range in {
			for k := range in[i] {
				if _, ok := seen[k]; !ok {
					if !yield(k) {
						return
					}
					seen[k] = struct{}{}
				}
			}
		}
	}
}

// Values creates a sequence of the map values.
// Play: https://go.dev/play/p/L9KcJ3h8E4f
func Values[K comparable, V any](in ...map[K]V) iter.Seq[V] {
	return func(yield func(V) bool) {
		for i := range in {
			for _, v := range in[i] {
				if !yield(v) {
					return
				}
			}
		}
	}
}

// UniqValues creates a sequence of unique values in the map.
// Will allocate a map large enough to hold all distinct input values.
// Long input sequences with heterogeneous values can cause excessive memory usage.
// Play: https://go.dev/play/p/M7qV2xP4yG8
func UniqValues[K, V comparable](in ...map[K]V) iter.Seq[V] {
	return func(yield func(V) bool) {
		seen := make(map[V]struct{})

		for i := range in {
			for _, v := range in[i] {
				if _, ok := seen[v]; !ok {
					if !yield(v) {
						return
					}
					seen[v] = struct{}{}
				}
			}
		}
	}
}

// Entries transforms a map into a sequence of key/value pairs.
// Play: https://go.dev/play/p/N8RbJ5t6H2k
func Entries[K comparable, V any](in ...map[K]V) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, m := range in {
			for k, v := range m {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// ToPairs transforms a map into a sequence of key/value pairs.
// Alias of Entries().
// Play: https://go.dev/play/p/N8RbJ5t6H2k
func ToPairs[K comparable, V any](in ...map[K]V) iter.Seq2[K, V] {
	return Entries(in...)
}

// FromEntries transforms a sequence of key/value pairs into a map.
// Play: https://go.dev/play/p/K3wL9j7TmXs
func FromEntries[K comparable, V any](entries ...iter.Seq2[K, V]) map[K]V {
	m := make(map[K]V)
	for _, e := range entries {
		maps.Insert(m, e)
	}
	return m
}

// FromPairs transforms a sequence of key/value pairs into a map.
// Alias of FromEntries().
// Play: https://go.dev/play/p/K3wL9j7TmXs
func FromPairs[K comparable, V any](entries ...iter.Seq2[K, V]) map[K]V {
	return FromEntries(entries...)
}

// Invert creates a sequence composed of inverted keys and values.
// Play: https://go.dev/play/p/H4jR7n2sF8k
func Invert[K, V comparable](in iter.Seq2[K, V]) iter.Seq2[V, K] {
	return func(yield func(V, K) bool) {
		for k, v := range in {
			if !yield(v, k) {
				return
			}
		}
	}
}

// Assign merges multiple sequences of maps from left to right.
// Play: https://go.dev/play/p/P6tY8qW3rN2
func Assign[K comparable, V any, Map ~map[K]V](maps ...iter.Seq[Map]) Map {
	out := make(Map)

	for i := range maps {
		for item := range maps[i] {
			for k, v := range item {
				out[k] = v
			}
		}
	}

	return out
}

// ChunkEntries splits a map into a sequence of elements in groups of length equal to its size. If the map cannot be split evenly,
// the final chunk will contain the remaining elements.
// Play: https://go.dev/play/p/Q4jR8m9T2nX
func ChunkEntries[K comparable, V any](m map[K]V, size int) iter.Seq[map[K]V] {
	if size <= 0 {
		panic("it.ChunkEntries: size must be greater than 0")
	}

	return func(yield func(map[K]V) bool) {
		var result map[K]V
		for k, v := range m {
			if result == nil {
				result = make(map[K]V, size)
			}
			result[k] = v
			if len(result) == size {
				if !yield(result) {
					return
				}
				result = nil
			}
		}
		if result != nil {
			yield(result)
		}
	}
}

// MapToSeq transforms a map into a sequence based on specified transform.
// Play: https://go.dev/play/p/R7sL5h4K3mV
func MapToSeq[K comparable, V, R any](in map[K]V, transform func(key K, value V) R) iter.Seq[R] {
	return func(yield func(R) bool) {
		for k, v := range in {
			if !yield(transform(k, v)) {
				return
			}
		}
	}
}

// FilterMapToSeq transforms a map into a sequence based on specified transform.
// The transform returns a value and a boolean. If the boolean is true, the value is added to the result sequence.
// If the boolean is false, the value is not added to the result sequence.
// The order of the keys in the input map is not specified and the order of the keys in the output sequence is not guaranteed.
// Play: https://go.dev/play/p/S6tY2uJ7nWq
func FilterMapToSeq[K comparable, V, R any](in map[K]V, transform func(key K, value V) (R, bool)) iter.Seq[R] {
	return func(yield func(R) bool) {
		for k, v := range in {
			if v, ok := transform(k, v); ok && !yield(v) {
				return
			}
		}
	}
}

// FilterKeys transforms a map into a sequence based on predicate returns true for specific elements.
// It is a mix of Filter and Keys.
// Play: https://go.dev/play/p/T8vW9kX7nLm
func FilterKeys[K comparable, V any](in map[K]V, predicate func(key K, value V) bool) iter.Seq[K] {
	return func(yield func(K) bool) {
		for k, v := range in {
			if predicate(k, v) && !yield(k) {
				return
			}
		}
	}
}

// FilterValues transforms a map into a sequence based on predicate returns true for specific elements.
// It is a mix of Filter and Values.
// Play: https://go.dev/play/p/U3yN7kV8oXp
func FilterValues[K comparable, V any](in map[K]V, predicate func(key K, value V) bool) iter.Seq[V] {
	return func(yield func(V) bool) {
		for k, v := range in {
			if predicate(k, v) && !yield(v) {
				return
			}
		}
	}
}

// SeqToSeq2 converts a sequence into a sequence of key-value pairs keyed by index.
// Play: https://go.dev/play/p/V5wL9xY8nQr
func SeqToSeq2[T any](in iter.Seq[T]) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		var i int
		for item := range in {
			if !yield(i, item) {
				return
			}
			i++
		}
	}
}

// Seq2KeyToSeq converts a sequence of key-value pairs into a sequence of keys.
// Play: https://go.dev/play/p/W6xM7zZ9oSt
func Seq2KeyToSeq[K, V any](in iter.Seq2[K, V]) iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range in {
			if !yield(k) {
				return
			}
		}
	}
}

// Seq2ValueToSeq converts a sequence of key-value pairs into a sequence of values.
// Play: https://go.dev/play/p/X7yN8aA1pUv
func Seq2ValueToSeq[K, V any](in iter.Seq2[K, V]) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range in {
			if !yield(v) {
				return
			}
		}
	}
}
