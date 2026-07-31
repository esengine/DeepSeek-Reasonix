package mutable

import "fmt"

func ExampleFilter() {
	list := []int{1, 2, 3, 4}

	newList := Filter(list, func(nbr int) bool {
		return nbr%2 == 0
	})

	fmt.Printf("%v\n%v", list, newList)
	// Output:
	// [2 4 3 4]
	// [2 4]
}

func ExampleFilterI() {
	list := []int{1, 2, 3, 4}

	newList := Filter(list, func(nbr int) bool {
		return nbr%2 == 0
	})

	fmt.Printf("%v\n%v", list, newList)
	// Output:
	// [2 4 3 4]
	// [2 4]
}

func ExampleMap() {
	list := []int{1, 2, 3, 4}

	Map(list, func(nbr int) int {
		return nbr * 2
	})

	fmt.Printf("%v", list)
	// Output: [2 4 6 8]
}

func ExampleMapI() {
	list := []int{1, 2, 3, 4}

	MapI(list, func(nbr, index int) int {
		return nbr * index
	})

	fmt.Printf("%v", list)
	// Output: [0 2 6 12]
}

func ExampleShuffle() {
	list := []int{0, 1, 2, 3, 4, 5}

	Shuffle(list)

	fmt.Printf("%v", list)
}

func ExampleReverse() {
	list := []int{0, 1, 2, 3, 4, 5}

	Reverse(list)

	fmt.Printf("%v", list)
	// Output: [5 4 3 2 1 0]
}

// Fill fills elements of a slice with `initial` value.
// Play: https://go.dev/play/p/VwR34GzqEub
func Fill[T any, Slice ~[]T](collection Slice, initial T) {
	for i := range collection {
		collection[i] = initial
	}
}
