//go:build go1.23

package it

import (
	"fmt"
	"slices"
)

func ExampleRange() {
	result1 := Range(4)
	result2 := Range(-4)
	result3 := RangeFrom(1, 5)
	result4 := RangeFrom(1.0, 5)
	result5 := RangeWithSteps(0, 20, 5)
	result6 := RangeWithSteps[float32](-1.0, -4.0, -1.0)
	result7 := RangeWithSteps(1, 4, -1)
	result8 := Range(0)

	fmt.Printf("%v\n", slices.Collect(result1))
	fmt.Printf("%v\n", slices.Collect(result2))
	fmt.Printf("%v\n", slices.Collect(result3))
	fmt.Printf("%v\n", slices.Collect(result4))
	fmt.Printf("%v\n", slices.Collect(result5))
	fmt.Printf("%v\n", slices.Collect(result6))
	fmt.Printf("%v\n", slices.Collect(result7))
	fmt.Printf("%v\n", slices.Collect(result8))
	// Output:
	// [0 1 2 3]
	// [0 -1 -2 -3]
	// [1 2 3 4 5]
	// [1 2 3 4 5]
	// [0 5 10 15]
	// [-1 -2 -3]
	// []
	// []
}

func ExampleSum() {
	ints := slices.Values([]int{1, 2, 3, 4, 5})

	sum := Sum(ints)

	fmt.Printf("%v", sum)
	// Output: 15
}

func ExampleSumBy() {
	ints := slices.Values([]string{"foo", "bar"})

	result := SumBy(ints, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 6
}

func ExampleProduct() {
	ints := slices.Values([]int{1, 2, 3, 4, 5})

	result := Product(ints)

	fmt.Printf("%v", result)
	// Output: 120
}

func ExampleProductBy() {
	strs := slices.Values([]string{"foo", "bar"})

	result := ProductBy(strs, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 9
}

func ExampleMean() {
	ints := slices.Values([]int{1, 2, 3, 4, 5})

	result := Mean(ints)

	fmt.Printf("%v", result)
	// Output: 3
}

func ExampleMeanBy() {
	strs := slices.Values([]string{"foo", "bar"})

	result := MeanBy(strs, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 3
}
