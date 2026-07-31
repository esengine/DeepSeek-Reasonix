//go:build go1.23

package it

import (
	"fmt"
	"slices"
)

func ExampleChunkString() {
	result1 := ChunkString("123456", 2)
	result2 := ChunkString("1234567", 2)
	result3 := ChunkString("", 2)
	result4 := ChunkString("1", 2)

	fmt.Printf("%v\n", slices.Collect(result1))
	fmt.Printf("%v\n", slices.Collect(result2))
	fmt.Printf("%v\n", slices.Collect(result3))
	fmt.Printf("%v\n", slices.Collect(result4))
	// Output:
	// [12 34 56]
	// [12 34 56 7]
	// []
	// [1]
}
