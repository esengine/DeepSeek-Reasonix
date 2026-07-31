//go:build go1.23

package it

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"strconv"
)

func ExampleLength() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := Length(list)

	fmt.Printf("%v", result)
	// Output: 4
}

func ExampleDrain() {
	list := slices.Values([]int64{1, 2, 3, 4})

	Drain(list)
}

func ExampleFilter() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := Filter(list, func(nbr int64) bool {
		return nbr%2 == 0
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 4]
}

func ExampleMap() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := Map(list, func(nbr int64) string {
		return strconv.FormatInt(nbr*2, 10)
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 4 6 8]
}

func ExampleUniqMap() {
	type User struct {
		Name string
		Age  int
	}
	users := slices.Values([]User{{Name: "Alex", Age: 10}, {Name: "Alex", Age: 12}, {Name: "Bob", Age: 11}, {Name: "Alice", Age: 20}})

	result := UniqMap(users, func(u User) string {
		return u.Name
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [Alex Bob Alice]
}

func ExampleFilterMap() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := FilterMap(list, func(nbr int64) (string, bool) {
		return strconv.FormatInt(nbr*2, 10), nbr%2 == 0
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [4 8]
}

func ExampleFlatMap() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := FlatMap(list, func(nbr int64) iter.Seq[string] {
		return slices.Values([]string{
			strconv.FormatInt(nbr, 10), // base 10
			strconv.FormatInt(nbr, 2),  // base 2
		})
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [1 1 2 10 3 11 4 100]
}

func ExampleReduce() {
	list := slices.Values([]int64{1, 2, 3, 4})

	result := Reduce(list, func(agg, item int64) int64 {
		return agg + item
	}, 0)

	fmt.Printf("%v", result)
	// Output: 10
}

func ExampleReduceLast() {
	list := slices.Values([][]int{{0, 1}, {2, 3}, {4, 5}})

	result := ReduceLast(list, func(agg, item []int) []int {
		return append(agg, item...)
	}, []int{})

	fmt.Printf("%v", result)
	// Output: [4 5 2 3 0 1]
}

func ExampleForEach() {
	list := slices.Values([]int64{1, 2, 3, 4})

	ForEach(list, func(x int64) {
		fmt.Println(x)
	})

	// Output:
	// 1
	// 2
	// 3
	// 4
}

func ExampleForEachWhile() {
	list := slices.Values([]int64{1, 2, -math.MaxInt, 4})

	ForEachWhile(list, func(x int64) bool {
		if x < 0 {
			return false
		}
		fmt.Println(x)
		return true
	})

	// Output:
	// 1
	// 2
}

func ExampleTimes() {
	result := Times(3, func(i int) string {
		return strconv.FormatInt(int64(i), 10)
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2]
}

func ExampleUniq() {
	list := slices.Values([]int{1, 2, 2, 1})

	result := Uniq(list)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [1 2]
}

func ExampleUniqBy() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := UniqBy(list, func(i int) int {
		return i % 3
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2]
}

func ExampleGroupBy() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := GroupBy(list, func(i int) int {
		return i % 3
	})

	fmt.Printf("%v\n", result[0])
	fmt.Printf("%v\n", result[1])
	fmt.Printf("%v\n", result[2])
	// Output:
	// [0 3]
	// [1 4]
	// [2 5]
}

func ExampleGroupByMap() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := GroupByMap(list, func(i int) (int, int) {
		return i % 3, i * 2
	})

	fmt.Printf("%v\n", result[0])
	fmt.Printf("%v\n", result[1])
	fmt.Printf("%v\n", result[2])
	// Output:
	// [0 6]
	// [2 8]
	// [4 10]
}

func ExampleChunk() {
	list := slices.Values([]int{0, 1, 2, 3, 4})

	result := Chunk(list, 2)

	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// [0 1]
	// [2 3]
	// [4]
}

func ExampleWindow() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := Window(list, 3)

	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// [1 2 3]
	// [2 3 4]
	// [3 4 5]
}

func ExampleSliding() {
	list := slices.Values([]int{1, 2, 3, 4, 5, 6, 7, 8})

	result := Sliding(list, 2, 3)

	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// [1 2]
	// [4 5]
	// [7 8]
}

func ExamplePartitionBy() {
	list := slices.Values([]int{-2, -1, 0, 1, 2, 3, 4})

	result := PartitionBy(list, func(x int) string {
		if x < 0 {
			return "negative"
		} else if x%2 == 0 {
			return "even"
		}
		return "odd"
	})

	for _, v := range result {
		fmt.Printf("%v\n", v)
	}
	// Output:
	// [-2 -1]
	// [0 2 4]
	// [1 3]
}

func ExampleFlatten() {
	list := []iter.Seq[int]{slices.Values([]int{0, 1, 2}), slices.Values([]int{3, 4, 5})}

	result := Flatten(list)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2 3 4 5]
}

func ExampleConcat() {
	list1 := slices.Values([]int{0, 1, 2})
	list2 := slices.Values([]int{3, 4, 5})
	list3 := slices.Values([]int{6, 7, 8})

	result := Concat(list1, list2, list3)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2 3 4 5 6 7 8]
}

func ExampleInterleave() {
	list1 := []iter.Seq[int]{slices.Values([]int{1, 4, 7}), slices.Values([]int{2, 5, 8}), slices.Values([]int{3, 6, 9})}
	list2 := []iter.Seq[int]{slices.Values([]int{1}), slices.Values([]int{2, 5, 8}), slices.Values([]int{3, 6}), slices.Values([]int{4, 7, 9, 10})}

	result1 := slices.Collect(Interleave(list1...))
	result2 := slices.Collect(Interleave(list2...))

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	// Output:
	// [1 2 3 4 5 6 7 8 9]
	// [1 2 3 4 5 6 7 8 9 10]
}

func ExampleShuffle() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := slices.Collect(Shuffle(list))

	fmt.Printf("%v", result)
}

func ExampleReverse() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := slices.Collect(Reverse(list))

	fmt.Printf("%v", result)
	// Output: [5 4 3 2 1 0]
}

func ExampleFill() {
	list := slices.Values([]foo{{"a"}, {"a"}})

	result := Fill(list, foo{"b"})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [{b} {b}]
}

func ExampleRepeat() {
	result := Repeat(2, foo{"a"})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [{a} {a}]
}

func ExampleRepeatBy() {
	result := RepeatBy(5, func(i int) string {
		return strconv.FormatInt(int64(math.Pow(float64(i), 2)), 10)
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 4 9 16]
}

func ExampleKeyBy() {
	list := slices.Values([]string{"a", "aa", "aaa"})

	result := KeyBy(list, func(str string) int {
		return len(str)
	})

	fmt.Printf("%v", result)
	// Output: map[1:a 2:aa 3:aaa]
}

func ExampleSeqToMap() {
	list := slices.Values([]string{"a", "aa", "aaa"})

	result := SeqToMap(list, func(str string) (string, int) {
		return str, len(str)
	})

	fmt.Printf("%v", result)
	// Output: map[a:1 aa:2 aaa:3]
}

func ExampleFilterSeqToMap() {
	list := slices.Values([]string{"a", "aa", "aaa"})

	result := FilterSeqToMap(list, func(str string) (string, int, bool) {
		return str, len(str), len(str) > 1
	})

	fmt.Printf("%v", result)
	// Output: map[aa:2 aaa:3]
}

func ExampleKeyify() {
	list := slices.Values([]string{"a", "a", "b", "b", "d"})

	set := Keyify(list)
	_, ok1 := set["a"]
	_, ok2 := set["c"]
	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", set)

	// Output:
	// true
	// false
	// map[a:{} b:{} d:{}]
}

func ExampleDrop() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := Drop(list, 2)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 3 4 5]
}

func ExampleTake() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := Take(list, 3)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2]
}

func ExampleDropWhile() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := DropWhile(list, func(val int) bool {
		return val < 2
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 3 4 5]
}

func ExampleTakeWhile() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := TakeWhile(list, func(val int) bool {
		return val < 3
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 2]
}

func ExampleDropByIndex() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := DropByIndex(list, 2)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [0 1 3 4 5]
}

func ExampleTakeFilter() {
	list := slices.Values([]int{1, 2, 3, 4, 5, 6})

	result := TakeFilter(list, 2, func(val int) bool {
		return val%2 == 0
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 4]
}

func ExampleTakeFilterI() {
	list := slices.Values([]int{1, 2, 3, 4, 5, 6})

	result := TakeFilterI(list, 2, func(val, index int) bool {
		return val%2 == 0 && index < 4
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 4]
}

func ExampleReject() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := Reject(list, func(x int) bool {
		return x%2 == 0
	})

	fmt.Printf("%v", slices.Collect(result))
	// Output: [1 3 5]
}

func ExampleCount() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3})

	result := Count(list, 2)

	fmt.Printf("%v", result)
	// Output: 2
}

func ExampleCountBy() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3})

	result := CountBy(list, func(i int) bool {
		return i < 4
	})

	fmt.Printf("%v", result)
	// Output: 8
}

func ExampleCountValues() {
	result1 := CountValues(slices.Values([]int{}))
	result2 := CountValues(slices.Values([]int{1, 2}))
	result3 := CountValues(slices.Values([]int{1, 2, 2}))
	result4 := CountValues(slices.Values([]string{"foo", "bar", ""}))
	result5 := CountValues(slices.Values([]string{"foo", "bar", "bar"}))

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	// Output:
	// map[]
	// map[1:1 2:1]
	// map[1:1 2:2]
	// map[:1 bar:1 foo:1]
	// map[bar:2 foo:1]
}

func ExampleCountValuesBy() {
	isEven := func(v int) bool {
		return v%2 == 0
	}

	result1 := CountValuesBy(slices.Values([]int{}), isEven)
	result2 := CountValuesBy(slices.Values([]int{1, 2}), isEven)
	result3 := CountValuesBy(slices.Values([]int{1, 2, 2}), isEven)

	length := func(v string) int {
		return len(v)
	}

	result4 := CountValuesBy(slices.Values([]string{"foo", "bar", ""}), length)
	result5 := CountValuesBy(slices.Values([]string{"foo", "bar", "bar"}), length)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	// Output:
	// map[]
	// map[false:1 true:1]
	// map[false:1 true:2]
	// map[0:1 3:2]
	// map[3:3]
}

func ExampleSubset() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5})

	result := Subset(list, 2, 3)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 3 4]
}

func ExampleSlice() {
	list := values(0, 1, 2, 3, 4, 5)

	result := Slice(list, 1, 4)
	fmt.Printf("%v\n", slices.Collect(result))

	result = Slice(list, 4, 1)
	fmt.Printf("%v\n", slices.Collect(result))

	result = Slice(list, 4, 5)
	fmt.Printf("%v\n", slices.Collect(result))

	// Output:
	// [1 2 3]
	// []
	// [4]
}

func ExampleReplace() {
	list := slices.Values([]int{0, 1, 0, 1, 2, 3, 0})

	result := Replace(list, 0, 42, 1)
	fmt.Printf("%v\n", slices.Collect(result))

	result = Replace(list, -1, 42, 1)
	fmt.Printf("%v\n", slices.Collect(result))

	result = Replace(list, 0, 42, 2)
	fmt.Printf("%v\n", slices.Collect(result))

	result = Replace(list, 0, 42, -1)
	fmt.Printf("%v\n", slices.Collect(result))

	// Output:
	// [42 1 0 1 2 3 0]
	// [0 1 0 1 2 3 0]
	// [42 1 42 1 2 3 0]
	// [42 1 42 1 2 3 42]
}

func ExampleCompact() {
	list := slices.Values([]string{"", "foo", "", "bar", ""})

	result := Compact(list)

	fmt.Printf("%v", slices.Collect(result))

	// Output: [foo bar]
}

func ExampleIsSorted() {
	list := slices.Values([]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})

	result := IsSorted(list)

	fmt.Printf("%v", result)

	// Output: true
}

func ExampleIsSortedBy() {
	list := slices.Values([]string{"a", "bb", "ccc"})

	result := IsSortedBy(list, func(s string) int {
		return len(s)
	})

	fmt.Printf("%v", result)

	// Output: true
}

func ExampleCutPrefix() {
	collection := slices.Values([]string{"a", "b", "c", "d", "e", "f", "g"})

	// Test with valid prefix
	after, found := CutPrefix(collection, []string{"a", "b", "c"})
	fmt.Printf("After: %v, Found: %t\n", slices.Collect(after), found)

	// Test with prefix not found
	after2, found2 := CutPrefix(collection, []string{"b"})
	fmt.Printf("After: %v, Found: %t\n", slices.Collect(after2), found2)

	// Test with empty prefix
	after3, found3 := CutPrefix(collection, []string{})
	fmt.Printf("After: %v, Found: %t\n", slices.Collect(after3), found3)

	// Output:
	// After: [d e f g], Found: true
	// After: [a b c d e f g], Found: false
	// After: [a b c d e f g], Found: true
}

func ExampleCutSuffix() {
	collection := slices.Values([]string{"a", "b", "c", "d", "e", "f", "g"})

	// Test with valid suffix
	before, found := CutSuffix(collection, []string{"f", "g"})
	fmt.Printf("Before: %v, Found: %t\n", slices.Collect(before), found)

	// Test with suffix not found
	before2, found2 := CutSuffix(collection, []string{"b"})
	fmt.Printf("Before: %v, Found: %t\n", slices.Collect(before2), found2)

	// Test with empty suffix
	before3, found3 := CutSuffix(collection, []string{})
	fmt.Printf("Before: %v, Found: %t\n", slices.Collect(before3), found3)

	// Output:
	// Before: [a b c d e], Found: true
	// Before: [a b c d e f g], Found: false
	// Before: [a b c d e f g], Found: true
}

func ExampleTrim() {
	collection := slices.Values([]int{0, 1, 2, 0, 3, 0})

	// Test with valid cutset
	result := Trim(collection, 0)
	fmt.Printf("Trim with cutset {0}: %v\n", slices.Collect(result))

	// Test with string collection
	words := slices.Values([]string{"  hello  ", "world", "  "})
	result2 := Trim(words, " ")
	fmt.Printf("Trim with string cutset: %v\n", slices.Collect(result2))

	// Test with no cutset elements
	result3 := Trim(collection, 5)
	fmt.Printf("Trim with cutset {5} (not present): %v\n", slices.Collect(result3))

	// Output:
	// Trim with cutset {0}: [1 2 0 3]
	// Trim with string cutset: [  hello   world   ]
	// Trim with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimFirst() {
	collection := slices.Values([]int{0, 1, 2, 0, 3, 0})

	// Test with valid cutset
	result := TrimFirst(collection, 0)
	fmt.Printf("TrimFirst with cutset {0}: %v\n", slices.Collect(result))

	// Test with string collection
	words := slices.Values([]string{"  hello  ", "world", "  "})
	result2 := TrimFirst(words, " ")
	fmt.Printf("TrimFirst with string cutset: %v\n", slices.Collect(result2))

	// Test with no cutset elements
	result3 := TrimFirst(collection, 5)
	fmt.Printf("TrimFirst with cutset {5} (not present): %v\n", slices.Collect(result3))

	// Output:
	// TrimFirst with cutset {0}: [1 2 0 3 0]
	// TrimFirst with string cutset: [  hello   world   ]
	// TrimFirst with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimPrefix() {
	collection := slices.Values([]int{1, 2, 1, 2, 3})

	// Test with valid prefix
	result := TrimPrefix(collection, []int{1, 2})
	fmt.Printf("TrimPrefix with prefix {1,2}: %v\n", slices.Collect(result))

	// Test with string collection
	words := slices.Values([]string{"hello", "hello", "world"})
	result2 := TrimPrefix(words, []string{"hello"})
	fmt.Printf("TrimPrefix with string prefix: %v\n", slices.Collect(result2))

	// Test with prefix not present
	result3 := TrimPrefix(collection, []int{5, 6})
	fmt.Printf("TrimPrefix with prefix {5,6} (not present): %v\n", slices.Collect(result3))

	// Output:
	// TrimPrefix with prefix {1,2}: [3]
	// TrimPrefix with string prefix: [world]
	// TrimPrefix with prefix {5,6} (not present): [1 2 1 2 3]
}

func ExampleTrimLast() {
	collection := slices.Values([]int{0, 1, 2, 0, 3, 0})

	// Test with valid cutset
	result := TrimLast(collection, 0)
	fmt.Printf("TrimLast with cutset {0}: %v\n", slices.Collect(result))

	// Test with string collection
	words := slices.Values([]string{"  hello  ", "world", "  "})
	result2 := TrimLast(words, " ")
	fmt.Printf("TrimLast with string cutset: %v\n", slices.Collect(result2))

	// Test with no cutset elements
	result3 := TrimLast(collection, 5)
	fmt.Printf("TrimLast with cutset {5} (not present): %v\n", slices.Collect(result3))

	// Output:
	// TrimLast with cutset {0}: [0 1 2 0 3]
	// TrimLast with string cutset: [  hello   world   ]
	// TrimLast with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimSuffix() {
	collection := slices.Values([]int{1, 2, 1, 2, 3})

	// Test with valid suffix
	result := TrimSuffix(collection, []int{1, 2})
	fmt.Printf("TrimSuffix with suffix {1,2}: %v\n", slices.Collect(result))

	// Test with string collection
	words := slices.Values([]string{"hello", "world", "test"})
	result2 := TrimSuffix(words, []string{"test"})
	fmt.Printf("TrimSuffix with string suffix: %v\n", slices.Collect(result2))

	// Test with suffix not present
	result3 := TrimSuffix(collection, []int{5, 6})
	fmt.Printf("TrimSuffix with suffix {5,6} (not present): %v\n", slices.Collect(result3))

	// Output:
	// TrimSuffix with suffix {1,2}: [1 2 1 2 3]
	// TrimSuffix with string suffix: [hello world]
	// TrimSuffix with suffix {5,6} (not present): [1 2 1 2 3]
}
