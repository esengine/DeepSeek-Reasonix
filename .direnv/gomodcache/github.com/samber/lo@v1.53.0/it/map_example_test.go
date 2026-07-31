//go:build go1.23

package it

import (
	"fmt"
	"maps"
	"slices"
	"sort"
)

func ExampleKeys() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 3}

	result := slices.Collect(Keys(kv, kv2))
	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [bar baz foo]
}

func ExampleUniqKeys() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"bar": 3}

	result := slices.Collect(UniqKeys(kv, kv2))
	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [bar foo]
}

func ExampleValues() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 3}

	result := slices.Collect(Values(kv, kv2))

	sort.Ints(result)
	fmt.Printf("%v", result)
	// Output: [1 2 3]
}

func ExampleUniqValues() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 2}

	result := slices.Collect(UniqValues(kv, kv2))

	sort.Ints(result)
	fmt.Printf("%v", result)
	// Output: [1 2]
}

func ExampleEntries() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := maps.Collect(Entries(kv))

	fmt.Printf("%v %v %v %v", len(result), result["foo"], result["bar"], result["baz"])
	// Output: 3 1 2 3
}

func ExampleFromEntries() {
	result := FromEntries(maps.All(map[string]int{
		"foo": 1,
		"bar": 2,
		"baz": 3,
	}))

	fmt.Printf("%v %v %v %v", len(result), result["foo"], result["bar"], result["baz"])
	// Output: 3 1 2 3
}

func ExampleInvert() {
	kv := maps.All(map[string]int{"foo": 1, "bar": 2, "baz": 3})

	result := maps.Collect(Invert(kv))

	fmt.Printf("%v %v %v %v", len(result), result[1], result[2], result[3])
	// Output: 3 foo bar baz
}

func ExampleAssign() {
	result := Assign(values(
		map[string]int{"a": 1, "b": 2},
		map[string]int{"b": 3, "c": 4},
	))

	fmt.Printf("%v %v %v %v", len(result), result["a"], result["b"], result["c"])
	// Output: 3 1 3 4
}

func ExampleChunkEntries() {
	result := ChunkEntries(
		map[string]int{
			"a": 1,
			"b": 2,
			"c": 3,
			"d": 4,
			"e": 5,
		},
		3,
	)

	for r := range result {
		fmt.Printf("%d\n", len(r))
	}
	// Output:
	// 3
	// 2
}

func ExampleMapToSeq() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	result := slices.Collect(MapToSeq(kv, func(k int, v int64) string {
		return fmt.Sprintf("%d_%d", k, v)
	}))

	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [1_1 2_2 3_3 4_4]
}

func ExampleFilterMapToSeq() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	result := slices.Collect(FilterMapToSeq(kv, func(k int, v int64) (string, bool) {
		return fmt.Sprintf("%d_%d", k, v), k%2 == 0
	}))

	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [2_2 4_4]
}

func ExampleFilterKeys() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result := slices.Collect(FilterKeys(kv, func(k int, v string) bool {
		return v == "foo"
	}))

	fmt.Printf("%v", result)
	// Output: [1]
}

func ExampleFilterValues() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result := slices.Collect(FilterValues(kv, func(k int, v string) bool {
		return v == "foo"
	}))

	fmt.Printf("%v", result)
	// Output: [foo]
}

func ExampleSeqToSeq2() {
	result := maps.Collect(SeqToSeq2(slices.Values([]string{"foo", "bar", "baz"})))

	fmt.Printf("%v %v %v %v", len(result), result[0], result[1], result[2])
	// Output: 3 foo bar baz
}

func ExampleSeq2KeyToSeq() {
	result := slices.Collect(Seq2KeyToSeq(maps.All(map[string]int{
		"foo": 1,
		"bar": 2,
		"baz": 3,
	})))

	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [bar baz foo]
}

func ExampleSeq2ValueToSeq() {
	result := slices.Collect(Seq2ValueToSeq(maps.All(map[string]int{
		"foo": 1,
		"bar": 2,
		"baz": 3,
	})))

	sort.Ints(result)
	fmt.Printf("%v", result)
	// Output: [1 2 3]
}
