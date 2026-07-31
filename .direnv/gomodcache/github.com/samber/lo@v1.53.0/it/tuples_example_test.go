//go:build go1.23

package it

import (
	"fmt"
	"slices"
)

func ExampleZip2() {
	result := Zip2(values("hello"), values(2))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2}]
}

func ExampleZip3() {
	result := Zip3(values("hello"), values(2), values(true))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true}]
}

func ExampleZip4() {
	result := Zip4(values("hello"), values(2), values(true), values(foo{bar: "bar"}))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar}}]
}

func ExampleZip5() {
	result := Zip5(values("hello"), values(2), values(true), values(foo{bar: "bar"}), values(4.2))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar} 4.2}]
}

func ExampleZip6() {
	result := Zip6(values("hello"), values(2), values(true), values(foo{bar: "bar"}), values(4.2), values("plop"))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar} 4.2 plop}]
}

func ExampleZip7() {
	result := Zip7(values("hello"), values(2), values(true), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar} 4.2 plop false}]
}

func ExampleZip8() {
	result := Zip8(values("hello"), values(2), values(true), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar} 4.2 plop false 42}]
}

func ExampleZip9() {
	result := Zip9(values("hello"), values(2), values(true), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42), values("hello world"))
	fmt.Printf("%v", slices.Collect(result))
	// Output: [{hello 2 true {bar} 4.2 plop false 42 hello world}]
}

func ExampleCrossJoin2() {
	result := CrossJoin2(values("a", "b"), values(1, 2, 3, 4))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1}
	// {a 2}
	// {a 3}
	// {a 4}
	// {b 1}
	// {b 2}
	// {b 3}
	// {b 4}
}

func ExampleCrossJoin3() {
	result := CrossJoin3(values("a", "b"), values(1, 2, 3, 4), values(true, false))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true}
	// {a 1 false}
	// {a 2 true}
	// {a 2 false}
	// {a 3 true}
	// {a 3 false}
	// {a 4 true}
	// {a 4 false}
	// {b 1 true}
	// {b 1 false}
	// {b 2 true}
	// {b 2 false}
	// {b 3 true}
	// {b 3 false}
	// {b 4 true}
	// {b 4 false}
}

func ExampleCrossJoin4() {
	result := CrossJoin4(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar}}
	// {a 1 false {bar}}
	// {a 2 true {bar}}
	// {a 2 false {bar}}
	// {a 3 true {bar}}
	// {a 3 false {bar}}
	// {a 4 true {bar}}
	// {a 4 false {bar}}
	// {b 1 true {bar}}
	// {b 1 false {bar}}
	// {b 2 true {bar}}
	// {b 2 false {bar}}
	// {b 3 true {bar}}
	// {b 3 false {bar}}
	// {b 4 true {bar}}
	// {b 4 false {bar}}
}

func ExampleCrossJoin5() {
	result := CrossJoin5(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar} 4.2}
	// {a 1 false {bar} 4.2}
	// {a 2 true {bar} 4.2}
	// {a 2 false {bar} 4.2}
	// {a 3 true {bar} 4.2}
	// {a 3 false {bar} 4.2}
	// {a 4 true {bar} 4.2}
	// {a 4 false {bar} 4.2}
	// {b 1 true {bar} 4.2}
	// {b 1 false {bar} 4.2}
	// {b 2 true {bar} 4.2}
	// {b 2 false {bar} 4.2}
	// {b 3 true {bar} 4.2}
	// {b 3 false {bar} 4.2}
	// {b 4 true {bar} 4.2}
	// {b 4 false {bar} 4.2}
}

func ExampleCrossJoin6() {
	result := CrossJoin6(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar} 4.2 plop}
	// {a 1 false {bar} 4.2 plop}
	// {a 2 true {bar} 4.2 plop}
	// {a 2 false {bar} 4.2 plop}
	// {a 3 true {bar} 4.2 plop}
	// {a 3 false {bar} 4.2 plop}
	// {a 4 true {bar} 4.2 plop}
	// {a 4 false {bar} 4.2 plop}
	// {b 1 true {bar} 4.2 plop}
	// {b 1 false {bar} 4.2 plop}
	// {b 2 true {bar} 4.2 plop}
	// {b 2 false {bar} 4.2 plop}
	// {b 3 true {bar} 4.2 plop}
	// {b 3 false {bar} 4.2 plop}
	// {b 4 true {bar} 4.2 plop}
	// {b 4 false {bar} 4.2 plop}
}

func ExampleCrossJoin7() {
	result := CrossJoin7(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar} 4.2 plop false}
	// {a 1 false {bar} 4.2 plop false}
	// {a 2 true {bar} 4.2 plop false}
	// {a 2 false {bar} 4.2 plop false}
	// {a 3 true {bar} 4.2 plop false}
	// {a 3 false {bar} 4.2 plop false}
	// {a 4 true {bar} 4.2 plop false}
	// {a 4 false {bar} 4.2 plop false}
	// {b 1 true {bar} 4.2 plop false}
	// {b 1 false {bar} 4.2 plop false}
	// {b 2 true {bar} 4.2 plop false}
	// {b 2 false {bar} 4.2 plop false}
	// {b 3 true {bar} 4.2 plop false}
	// {b 3 false {bar} 4.2 plop false}
	// {b 4 true {bar} 4.2 plop false}
	// {b 4 false {bar} 4.2 plop false}
}

func ExampleCrossJoin8() {
	result := CrossJoin8(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar} 4.2 plop false 42}
	// {a 1 false {bar} 4.2 plop false 42}
	// {a 2 true {bar} 4.2 plop false 42}
	// {a 2 false {bar} 4.2 plop false 42}
	// {a 3 true {bar} 4.2 plop false 42}
	// {a 3 false {bar} 4.2 plop false 42}
	// {a 4 true {bar} 4.2 plop false 42}
	// {a 4 false {bar} 4.2 plop false 42}
	// {b 1 true {bar} 4.2 plop false 42}
	// {b 1 false {bar} 4.2 plop false 42}
	// {b 2 true {bar} 4.2 plop false 42}
	// {b 2 false {bar} 4.2 plop false 42}
	// {b 3 true {bar} 4.2 plop false 42}
	// {b 3 false {bar} 4.2 plop false 42}
	// {b 4 true {bar} 4.2 plop false 42}
	// {b 4 false {bar} 4.2 plop false 42}
}

func ExampleCrossJoin9() {
	result := CrossJoin9(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42), values("hello world"))
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// {a 1 true {bar} 4.2 plop false 42 hello world}
	// {a 1 false {bar} 4.2 plop false 42 hello world}
	// {a 2 true {bar} 4.2 plop false 42 hello world}
	// {a 2 false {bar} 4.2 plop false 42 hello world}
	// {a 3 true {bar} 4.2 plop false 42 hello world}
	// {a 3 false {bar} 4.2 plop false 42 hello world}
	// {a 4 true {bar} 4.2 plop false 42 hello world}
	// {a 4 false {bar} 4.2 plop false 42 hello world}
	// {b 1 true {bar} 4.2 plop false 42 hello world}
	// {b 1 false {bar} 4.2 plop false 42 hello world}
	// {b 2 true {bar} 4.2 plop false 42 hello world}
	// {b 2 false {bar} 4.2 plop false 42 hello world}
	// {b 3 true {bar} 4.2 plop false 42 hello world}
	// {b 3 false {bar} 4.2 plop false 42 hello world}
	// {b 4 true {bar} 4.2 plop false 42 hello world}
	// {b 4 false {bar} 4.2 plop false 42 hello world}
}

func ExampleCrossJoinBy2() {
	result := CrossJoinBy2(values("a", "b"), values(1, 2, 3, 4), func(a string, b int) string {
		return fmt.Sprintf("%v-%v", a, b)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1
	// a-2
	// a-3
	// a-4
	// b-1
	// b-2
	// b-3
	// b-4
}

func ExampleCrossJoinBy3() {
	result := CrossJoinBy3(values("a", "b"), values(1, 2, 3, 4), values(true, false), func(a string, b int, c bool) string {
		return fmt.Sprintf("%v-%v-%v", a, b, c)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true
	// a-1-false
	// a-2-true
	// a-2-false
	// a-3-true
	// a-3-false
	// a-4-true
	// a-4-false
	// b-1-true
	// b-1-false
	// b-2-true
	// b-2-false
	// b-3-true
	// b-3-false
	// b-4-true
	// b-4-false
}

func ExampleCrossJoinBy4() {
	result := CrossJoinBy4(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), func(a string, b int, c bool, d foo) string {
		return fmt.Sprintf("%v-%v-%v-%v", a, b, c, d)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}
	// a-1-false-{bar}
	// a-2-true-{bar}
	// a-2-false-{bar}
	// a-3-true-{bar}
	// a-3-false-{bar}
	// a-4-true-{bar}
	// a-4-false-{bar}
	// b-1-true-{bar}
	// b-1-false-{bar}
	// b-2-true-{bar}
	// b-2-false-{bar}
	// b-3-true-{bar}
	// b-3-false-{bar}
	// b-4-true-{bar}
	// b-4-false-{bar}
}

func ExampleCrossJoinBy5() {
	result := CrossJoinBy5(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), func(a string, b int, c bool, d foo, e float64) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v", a, b, c, d, e)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}-4.2
	// a-1-false-{bar}-4.2
	// a-2-true-{bar}-4.2
	// a-2-false-{bar}-4.2
	// a-3-true-{bar}-4.2
	// a-3-false-{bar}-4.2
	// a-4-true-{bar}-4.2
	// a-4-false-{bar}-4.2
	// b-1-true-{bar}-4.2
	// b-1-false-{bar}-4.2
	// b-2-true-{bar}-4.2
	// b-2-false-{bar}-4.2
	// b-3-true-{bar}-4.2
	// b-3-false-{bar}-4.2
	// b-4-true-{bar}-4.2
	// b-4-false-{bar}-4.2
}

func ExampleCrossJoinBy6() {
	result := CrossJoinBy6(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), func(a string, b int, c bool, d foo, e float64, f string) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v", a, b, c, d, e, f)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}-4.2-plop
	// a-1-false-{bar}-4.2-plop
	// a-2-true-{bar}-4.2-plop
	// a-2-false-{bar}-4.2-plop
	// a-3-true-{bar}-4.2-plop
	// a-3-false-{bar}-4.2-plop
	// a-4-true-{bar}-4.2-plop
	// a-4-false-{bar}-4.2-plop
	// b-1-true-{bar}-4.2-plop
	// b-1-false-{bar}-4.2-plop
	// b-2-true-{bar}-4.2-plop
	// b-2-false-{bar}-4.2-plop
	// b-3-true-{bar}-4.2-plop
	// b-3-false-{bar}-4.2-plop
	// b-4-true-{bar}-4.2-plop
	// b-4-false-{bar}-4.2-plop
}

func ExampleCrossJoinBy7() {
	result := CrossJoinBy7(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), func(a string, b int, c bool, d foo, e float64, f string, g bool) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}-4.2-plop-false
	// a-1-false-{bar}-4.2-plop-false
	// a-2-true-{bar}-4.2-plop-false
	// a-2-false-{bar}-4.2-plop-false
	// a-3-true-{bar}-4.2-plop-false
	// a-3-false-{bar}-4.2-plop-false
	// a-4-true-{bar}-4.2-plop-false
	// a-4-false-{bar}-4.2-plop-false
	// b-1-true-{bar}-4.2-plop-false
	// b-1-false-{bar}-4.2-plop-false
	// b-2-true-{bar}-4.2-plop-false
	// b-2-false-{bar}-4.2-plop-false
	// b-3-true-{bar}-4.2-plop-false
	// b-3-false-{bar}-4.2-plop-false
	// b-4-true-{bar}-4.2-plop-false
	// b-4-false-{bar}-4.2-plop-false
}

func ExampleCrossJoinBy8() {
	result := CrossJoinBy8(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42), func(a string, b int, c bool, d foo, e float64, f string, g bool, h int) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g, h)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}-4.2-plop-false-42
	// a-1-false-{bar}-4.2-plop-false-42
	// a-2-true-{bar}-4.2-plop-false-42
	// a-2-false-{bar}-4.2-plop-false-42
	// a-3-true-{bar}-4.2-plop-false-42
	// a-3-false-{bar}-4.2-plop-false-42
	// a-4-true-{bar}-4.2-plop-false-42
	// a-4-false-{bar}-4.2-plop-false-42
	// b-1-true-{bar}-4.2-plop-false-42
	// b-1-false-{bar}-4.2-plop-false-42
	// b-2-true-{bar}-4.2-plop-false-42
	// b-2-false-{bar}-4.2-plop-false-42
	// b-3-true-{bar}-4.2-plop-false-42
	// b-3-false-{bar}-4.2-plop-false-42
	// b-4-true-{bar}-4.2-plop-false-42
	// b-4-false-{bar}-4.2-plop-false-42
}

func ExampleCrossJoinBy9() {
	result := CrossJoinBy9(values("a", "b"), values(1, 2, 3, 4), values(true, false), values(foo{bar: "bar"}), values(4.2), values("plop"), values(false), values(42), values("hello world"), func(a string, b int, c bool, d foo, e float64, f string, g bool, h int, i string) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g, h, i)
	})
	for r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// a-1-true-{bar}-4.2-plop-false-42-hello world
	// a-1-false-{bar}-4.2-plop-false-42-hello world
	// a-2-true-{bar}-4.2-plop-false-42-hello world
	// a-2-false-{bar}-4.2-plop-false-42-hello world
	// a-3-true-{bar}-4.2-plop-false-42-hello world
	// a-3-false-{bar}-4.2-plop-false-42-hello world
	// a-4-true-{bar}-4.2-plop-false-42-hello world
	// a-4-false-{bar}-4.2-plop-false-42-hello world
	// b-1-true-{bar}-4.2-plop-false-42-hello world
	// b-1-false-{bar}-4.2-plop-false-42-hello world
	// b-2-true-{bar}-4.2-plop-false-42-hello world
	// b-2-false-{bar}-4.2-plop-false-42-hello world
	// b-3-true-{bar}-4.2-plop-false-42-hello world
	// b-3-false-{bar}-4.2-plop-false-42-hello world
	// b-4-true-{bar}-4.2-plop-false-42-hello world
	// b-4-false-{bar}-4.2-plop-false-42-hello world
}
