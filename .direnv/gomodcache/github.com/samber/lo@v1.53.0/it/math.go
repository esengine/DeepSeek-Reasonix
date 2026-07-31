//go:build go1.23

package it

import (
	"iter"

	"github.com/samber/lo"
	"github.com/samber/lo/internal/constraints"
)

// Range creates a sequence of numbers (positive and/or negative) with given length.
// Play: https://go.dev/play/p/6ksL0W6KEuQ
func Range(elementNum int) iter.Seq[int] {
	step := lo.Ternary(elementNum < 0, -1, 1)
	length := elementNum * step
	return func(yield func(int) bool) {
		for i, j := 0, 0; i < length; i, j = i+1, j+step {
			if !yield(j) {
				return
			}
		}
	}
}

// RangeFrom creates a sequence of numbers from start with specified length.
// Play: https://go.dev/play/p/WHP_NI5scj9
func RangeFrom[T constraints.Integer | constraints.Float](start T, elementNum int) iter.Seq[T] {
	step := lo.Ternary(elementNum < 0, -1, 1)
	length := elementNum * step
	return func(yield func(T) bool) {
		for i, j := 0, start; i < length; i, j = i+1, j+T(step) {
			if !yield(j) {
				return
			}
		}
	}
}

// RangeWithSteps creates a sequence of numbers (positive and/or negative) progressing from start up to, but not including end.
// step set to zero will return an empty sequence.
// Play: https://go.dev/play/p/qxm2YNLG0vT
func RangeWithSteps[T constraints.Integer | constraints.Float](start, end, step T) iter.Seq[T] {
	return func(yield func(T) bool) {
		if start == end || step == 0 {
			return
		}
		if start < end {
			if step < 0 {
				return
			}
			for i := start; i < end; i += step {
				if !yield(i) {
					return
				}
			}
		}
		if step > 0 {
			return
		}
		for i := start; i > end; i += step {
			if !yield(i) {
				return
			}
		}
	}
}

// Sum sums the values in a collection. If collection is empty 0 is returned.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/nHbGFOEIeTa
func Sum[T constraints.Float | constraints.Integer | constraints.Complex](collection iter.Seq[T]) T {
	return SumBy(collection, func(item T) T { return item })
}

// SumBy summarizes the values in a collection using the given return value from the iteration function. If collection is empty 0 is returned.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/ZNiqXNMu5QP
func SumBy[T any, R constraints.Float | constraints.Integer | constraints.Complex](collection iter.Seq[T], transform func(item T) R) R {
	var sum R
	for item := range collection {
		sum += transform(item)
	}
	return sum
}

// Product gets the product of the values in a collection. If collection is empty 1 is returned.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/ebgxKxJmhLj
func Product[T constraints.Float | constraints.Integer | constraints.Complex](collection iter.Seq[T]) T {
	return ProductBy(collection, func(item T) T { return item })
}

// ProductBy summarizes the values in a collection using the given return value from the iteration function. If collection is empty 1 is returned.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/BkRYS0NG42b
func ProductBy[T any, R constraints.Float | constraints.Integer | constraints.Complex](collection iter.Seq[T], transform func(item T) R) R {
	var product R = 1
	for item := range collection {
		product *= transform(item)
	}
	return product
}

// Mean calculates the mean of a collection of numbers.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/Lez0CsvVRl_l
func Mean[T constraints.Float | constraints.Integer](collection iter.Seq[T]) T {
	return MeanBy(collection, func(item T) T { return item })
}

// MeanBy calculates the mean of a collection of numbers using the given return value from the iteration function.
// Will iterate through the entire sequence.
// Play: https://go.dev/play/p/Ked4rpztH5Y
func MeanBy[T any, R constraints.Float | constraints.Integer](collection iter.Seq[T], transform func(item T) R) R {
	var sum R
	var length R
	for item := range collection {
		sum += transform(item)
		length++
	}
	if length == 0 {
		return 0
	}
	return sum / length
}

// Mode returns the mode (most frequent value) of a collection.
// If multiple values have the same highest frequency, then multiple values are returned.
// If the collection is empty, then the zero value of T is returned.
// Will iterate through the entire sequence and allocate a map large enough to hold all distinct elements.
// Long heterogeneous input sequences can cause excessive memory usage.
// Play: https://go.dev/play/p/c_cmMMA5EhH
func Mode[T constraints.Integer | constraints.Float](collection iter.Seq[T]) []T {
	mode := make([]T, 0)
	maxFreq := 0
	frequency := make(map[T]int)

	for item := range collection {
		frequency[item]++
		count := frequency[item]

		if count > maxFreq {
			maxFreq = count
			mode = append(mode[:0], item)
		} else if count == maxFreq {
			mode = append(mode, item)
		}
	}

	return mode[:len(mode):len(mode)]
}
