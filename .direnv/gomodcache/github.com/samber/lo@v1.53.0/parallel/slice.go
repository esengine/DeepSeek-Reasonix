package parallel

import "sync"

// Map manipulates a slice and transforms it to a slice of another type.
// `transform` is called in parallel. Result keep the same order.
// Play: https://go.dev/play/p/sCJaB3quRMC
func Map[T, R any](collection []T, transform func(item T, index int) R) []R {
	result := make([]R, len(collection))

	var wg sync.WaitGroup
	wg.Add(len(collection))

	for i, item := range collection {
		go func(_item T, _i int) {
			res := transform(_item, _i)

			result[_i] = res

			wg.Done()
		}(item, i)
	}

	wg.Wait()

	return result
}

// ForEach iterates over elements of collection and invokes callback for each element.
// `iteratee` is called in parallel.
// Play: https://go.dev/play/p/sCJaB3quRMC
func ForEach[T any](collection []T, callback func(item T, index int)) {
	var wg sync.WaitGroup
	wg.Add(len(collection))

	for i, item := range collection {
		go func(_item T, _i int) {
			callback(_item, _i)
			wg.Done()
		}(item, i)
	}

	wg.Wait()
}

// Times invokes the iteratee n times, returning a slice of the results of each invocation.
// The iteratee is invoked with index as argument.
// `iteratee` is called in parallel.
func Times[T any](count int, iteratee func(index int) T) []T {
	result := make([]T, count)

	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(_i int) {
			item := iteratee(_i)

			result[_i] = item

			wg.Done()
		}(i)
	}

	wg.Wait()

	return result
}

// GroupBy returns an object composed of keys generated from the results of running each element of collection through iteratee.
// The order of grouped values is determined by the order they occur in the collection.
// `iteratee` is called in parallel.
func GroupBy[T any, U comparable, Slice ~[]T](collection Slice, iteratee func(item T) U) map[U]Slice {
	result := map[U]Slice{}

	keys := Map(collection, func(item T, _ int) U {
		return iteratee(item)
	})

	for i, item := range collection {
		result[keys[i]] = append(result[keys[i]], item)
	}

	return result
}

// PartitionBy returns a slice of elements split into groups. The order of grouped values is
// determined by the order they occur in collection. The grouping is generated from the results
// of running each element of collection through iteratee.
// The order of groups is determined by their first appearance in the collection.
// `iteratee` is called in parallel.
func PartitionBy[T any, K comparable, Slice ~[]T](collection Slice, iteratee func(item T) K) []Slice {
	result := []Slice{}
	seen := map[K]int{}

	keys := Map(collection, func(item T, _ int) K {
		return iteratee(item)
	})

	for i := range collection {
		resultIndex, ok := seen[keys[i]]
		if ok {
			result[resultIndex] = append(result[resultIndex], collection[i])
		} else {
			seen[keys[i]] = len(result)
			result = append(result, Slice{collection[i]})
		}
	}

	return result
}
