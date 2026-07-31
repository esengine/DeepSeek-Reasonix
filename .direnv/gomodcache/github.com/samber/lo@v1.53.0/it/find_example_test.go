//go:build go1.23

package it

import (
	"fmt"
	"slices"
	"time"
)

func ExampleIndexOf() {
	list := slices.Values([]string{"foo", "bar", "baz"})

	result := IndexOf(list, "bar")

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleIndexOf_notFound() {
	list := slices.Values([]string{"foo", "bar", "baz"})

	result := IndexOf(list, "qux")

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleLastIndexOf() {
	list := slices.Values([]string{"foo", "bar", "baz", "bar"})

	result := LastIndexOf(list, "bar")

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleLastIndexOf_notFound() {
	list := slices.Values([]string{"foo", "bar", "baz"})

	result := LastIndexOf(list, "qux")

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleFind() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	})

	result, found := Find(users, func(user User) bool {
		return user.Age > 30
	})

	fmt.Printf("%s %t", result.Name, found)
	// Output: Charlie true
}

func ExampleFind_notFound() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, found := Find(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleFindIndexOf() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, index, found := FindIndexOf(list, func(n int) bool {
		return n > 2
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 3 2 true
}

func ExampleFindIndexOf_notFound() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, index, found := FindIndexOf(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 0 -1 false
}

func ExampleFindLastIndexOf() {
	list := slices.Values([]int{1, 2, 3, 4, 3, 5})

	result, index, found := FindLastIndexOf(list, func(n int) bool {
		return n == 3
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 3 4 true
}

func ExampleFindLastIndexOf_notFound() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, index, found := FindLastIndexOf(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 0 -1 false
}

func ExampleFindOrElse() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := FindOrElse(list, -1, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleFindOrElse_found() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := FindOrElse(list, -1, func(n int) bool {
		return n > 3
	})

	fmt.Printf("%d", result)
	// Output: 4
}

func ExampleFindUniques() {
	list := slices.Values([]int{1, 2, 2, 3, 3, 3, 4, 5})

	result := FindUniques(list)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [1 4 5]
}

func ExampleFindUniquesBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 25},
		{Name: "David", Age: 30},
		{Name: "Eve", Age: 35},
	})

	result := FindUniquesBy(users, func(user User) int {
		return user.Age
	})

	fmt.Printf("%d", len(slices.Collect(result)))
	// Output: 1
}

func ExampleFindDuplicates() {
	list := slices.Values([]int{1, 2, 2, 3, 3, 3, 4, 5})

	result := FindDuplicates(list)

	fmt.Printf("%v", slices.Collect(result))
	// Output: [2 3]
}

func ExampleFindDuplicatesBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 25},
		{Name: "David", Age: 30},
		{Name: "Eve", Age: 35},
	})

	result := FindDuplicatesBy(users, func(user User) int {
		return user.Age
	})

	fmt.Printf("%d", len(slices.Collect(result)))
	// Output: 2
}

func ExampleMin() {
	list := slices.Values([]int{3, 1, 4, 1, 5, 9, 2, 6})

	result := Min(list)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleMin_empty() {
	list := slices.Values([]int{})

	result := Min(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleMinIndex() {
	list := slices.Values([]int{3, 1, 4, 1, 5, 9, 2, 6})

	result, index := MinIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 1 1
}

func ExampleMinIndex_empty() {
	list := slices.Values([]int{})

	result, index := MinIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 0 -1
}

func ExampleMinBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	})

	result := MinBy(users, func(a, b User) bool {
		return a.Age < b.Age
	})

	fmt.Printf("%s", result.Name)
	// Output: Alice
}

func ExampleMinIndexBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	})

	result, index := MinIndexBy(users, func(a, b User) bool {
		return a.Age < b.Age
	})

	fmt.Printf("%s %d", result.Name, index)
	// Output: Alice 0
}

func ExampleEarliest() {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	result := Earliest(slices.Values([]time.Time{future, now, past}))

	fmt.Printf("%t", result.Equal(past))
	// Output: true
}

func ExampleEarliestBy() {
	type Event struct {
		Name string
		Time time.Time
	}

	now := time.Now()
	events := slices.Values([]Event{
		{Name: "Event A", Time: now.Add(time.Hour)},
		{Name: "Event B", Time: now},
		{Name: "Event C", Time: now.Add(-time.Hour)},
	})

	result := EarliestBy(events, func(event Event) time.Time {
		return event.Time
	})

	fmt.Printf("%s", result.Name)
	// Output: Event C
}

func ExampleMax() {
	list := slices.Values([]int{3, 1, 4, 1, 5, 9, 2, 6})

	result := Max(list)

	fmt.Printf("%d", result)
	// Output: 9
}

func ExampleMax_empty() {
	list := slices.Values([]int{})

	result := Max(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleMaxIndex() {
	list := slices.Values([]int{3, 1, 4, 1, 5, 9, 2, 6})

	result, index := MaxIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 9 5
}

func ExampleMaxIndex_empty() {
	list := slices.Values([]int{})

	result, index := MaxIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 0 -1
}

func ExampleMaxBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	})

	result := MaxBy(users, func(a, b User) bool {
		return a.Age > b.Age
	})

	fmt.Printf("%s", result.Name)
	// Output: Charlie
}

func ExampleMaxIndexBy() {
	type User struct {
		Name string
		Age  int
	}

	users := slices.Values([]User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	})

	result, index := MaxIndexBy(users, func(a, b User) bool {
		return a.Age > b.Age
	})

	fmt.Printf("%s %d", result.Name, index)
	// Output: Charlie 2
}

func ExampleLatest() {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	result := Latest(slices.Values([]time.Time{future, now, past}))

	fmt.Printf("%t", result.Equal(future))
	// Output: true
}

func ExampleLatestBy() {
	type Event struct {
		Name string
		Time time.Time
	}

	now := time.Now()
	events := slices.Values([]Event{
		{Name: "Event A", Time: now.Add(time.Hour)},
		{Name: "Event B", Time: now},
		{Name: "Event C", Time: now.Add(-time.Hour)},
	})

	result := LatestBy(events, func(event Event) time.Time {
		return event.Time
	})

	fmt.Printf("%s", result.Name)
	// Output: Event A
}

func ExampleFirst() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, found := First(list)

	fmt.Printf("%d %t", result, found)
	// Output: 1 true
}

func ExampleFirst_empty() {
	list := slices.Values([]int{})

	result, found := First(list)

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleFirstOrEmpty() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := FirstOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleFirstOrEmpty_empty() {
	list := slices.Values([]int{})

	result := FirstOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleFirstOr() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := FirstOr(list, -1)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleFirstOr_empty() {
	list := slices.Values([]int{})

	result := FirstOr(list, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleLast() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, found := Last(list)

	fmt.Printf("%d %t", result, found)
	// Output: 5 true
}

func ExampleLast_empty() {
	list := slices.Values([]int{})

	result, found := Last(list)

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleLastOrEmpty() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := LastOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 5
}

func ExampleLastOrEmpty_empty() {
	list := slices.Values([]int{})

	result := LastOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleLastOr() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := LastOr(list, -1)

	fmt.Printf("%d", result)
	// Output: 5
}

func ExampleLastOr_empty() {
	list := slices.Values([]int{})

	result := LastOr(list, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleNth() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, err := Nth(list, 2)

	fmt.Printf("%d %v", result, err)
	// Output: 3 <nil>
}

func ExampleNth_outOfBounds() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result, err := Nth(list, 10)

	fmt.Printf("%d %v", result, err)
	// Output: 0 nth: 10 out of bounds
}

func ExampleNthOr() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := NthOr(list, 2, -1)

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleNthOr_outOfBounds() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := NthOr(list, 10, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleNthOrEmpty() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := NthOrEmpty(list, 2)

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleNthOrEmpty_outOfBounds() {
	list := slices.Values([]int{1, 2, 3, 4, 5})

	result := NthOrEmpty(list, 10)

	fmt.Printf("%d", result)
	// Output: 0
}
