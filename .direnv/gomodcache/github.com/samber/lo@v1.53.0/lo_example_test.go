package lo

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ExampleTernary() {
	result := Ternary(true, "a", "b")

	fmt.Printf("%v", result)
	// Output: a
}

func ExampleTernaryF() {
	result := TernaryF(true, func() string { return "a" }, func() string { return "b" })

	fmt.Printf("%v", result)
	// Output: a
}

func ExampleIf() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func ExampleIfF() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_ifElse_ElseIf() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_ifElse_ElseIfF() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_ifElse_Else() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_ifElse_ElseF() {
	result1 := If(true, 1).
		ElseIf(false, 2).
		Else(3)

	result2 := If(false, 1).
		ElseIf(true, 2).
		Else(3)

	result3 := If(false, 1).
		ElseIf(false, 2).
		Else(3)

	result4 := IfF(true, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result5 := IfF(false, func() int { return 1 }).
		ElseIfF(true, func() int { return 2 }).
		ElseF(func() int { return 3 })

	result6 := IfF(false, func() int { return 1 }).
		ElseIfF(false, func() int { return 2 }).
		ElseF(func() int { return 3 })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func ExampleSwitch() {
	result1 := Switch[int, string](1).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result2 := Switch[int, string](2).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result3 := Switch[int, string](42).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result4 := Switch[int, string](1).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result5 := Switch[int, string](2).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result6 := Switch[int, string](42).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_switchCase_Case() {
	result1 := Switch[int, string](1).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result2 := Switch[int, string](2).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result3 := Switch[int, string](42).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result4 := Switch[int, string](1).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result5 := Switch[int, string](2).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result6 := Switch[int, string](42).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_switchCase_CaseF() {
	result1 := Switch[int, string](1).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result2 := Switch[int, string](2).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result3 := Switch[int, string](42).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result4 := Switch[int, string](1).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result5 := Switch[int, string](2).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result6 := Switch[int, string](42).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_switchCase_Default() {
	result1 := Switch[int, string](1).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result2 := Switch[int, string](2).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result3 := Switch[int, string](42).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result4 := Switch[int, string](1).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result5 := Switch[int, string](2).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result6 := Switch[int, string](42).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func Example_switchCase_DefaultF() {
	result1 := Switch[int, string](1).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result2 := Switch[int, string](2).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result3 := Switch[int, string](42).
		Case(1, "1").
		Case(2, "2").
		Default("3")

	result4 := Switch[int, string](1).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result5 := Switch[int, string](2).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	result6 := Switch[int, string](42).
		CaseF(1, func() string { return "1" }).
		CaseF(2, func() string { return "2" }).
		DefaultF(func() string { return "3" })

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	// Output:
	// 1
	// 2
	// 3
	// 1
	// 2
	// 3
}

func ExampleValidate() {
	i := 42

	err1 := Validate(i < 0, "expected %d < 0", i)
	err2 := Validate(i > 0, "expected %d > 0", i)

	fmt.Printf("%v\n%v", err1, err2)
	// Output:
	// expected 42 < 0
	// <nil>
}

func ExampleMust() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must(42, nil)

	// won't panic
	cb := func() (int, error) {
		return 42, nil
	}
	Must(cb())

	// will panic
	Must(42, errors.New("my error"))

	// will panic with error message
	Must(42, errors.New("world"), "hello")
}

func ExampleMust0() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must0(nil)

	// will panic
	Must0(errors.New("my error"))

	// will panic with error message
	Must0(errors.New("world"), "hello")
}

func ExampleMust1() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must1(42, nil)

	// won't panic
	cb := func() (int, error) {
		return 42, nil
	}
	Must1(cb())

	// will panic
	Must1(42, errors.New("my error"))

	// will panic with error message
	Must1(42, errors.New("world"), "hello")
}

func ExampleMust2() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must2(42, "hello", nil)

	// will panic
	Must2(42, "hello", errors.New("my error"))

	// will panic with error message
	Must2(42, "hello", errors.New("world"), "hello")
}

func ExampleMust3() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must3(42, "hello", 4.2, nil)

	// will panic
	Must3(42, "hello", 4.2, errors.New("my error"))

	// will panic with error message
	Must3(42, "hello", 4.2, errors.New("world"), "hello")
}

func ExampleMust4() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must4(42, "hello", 4.2, true, nil)

	// will panic
	Must4(42, "hello", 4.2, true, errors.New("my error"))

	// will panic with error message
	Must4(42, "hello", 4.2, true, errors.New("world"), "hello")
}

func ExampleMust5() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must5(42, "hello", 4.2, true, foo{}, nil)

	// will panic
	Must5(42, "hello", 4.2, true, foo{}, errors.New("my error"))

	// will panic with error message
	Must5(42, "hello", 4.2, true, foo{}, errors.New("world"), "hello")
}

func ExampleMust6() {
	defer func() {
		_ = recover()
	}()

	// won't panic
	Must5(42, "hello", 4.2, true, foo{}, "foobar", nil)

	// will panic
	Must5(42, "hello", 4.2, true, foo{}, "foobar", errors.New("my error"))

	// will panic with error message
	Must5(42, "hello", 4.2, true, foo{}, "foobar", errors.New("world"), "hello")
}

func ExampleTry() {
	ok1 := Try(func() error {
		return nil
	})
	ok2 := Try(func() error {
		return errors.New("my error")
	})
	ok3 := Try(func() error {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry1() {
	ok1 := Try1(func() error {
		return nil
	})
	ok2 := Try1(func() error {
		return errors.New("my error")
	})
	ok3 := Try1(func() error {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry2() {
	ok1 := Try2(func() (int, error) {
		return 42, nil
	})
	ok2 := Try2(func() (int, error) {
		return 42, errors.New("my error")
	})
	ok3 := Try2(func() (int, error) {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry3() {
	ok1 := Try3(func() (int, string, error) {
		return 42, "foobar", nil
	})
	ok2 := Try3(func() (int, string, error) {
		return 42, "foobar", errors.New("my error")
	})
	ok3 := Try3(func() (int, string, error) {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry4() {
	ok1 := Try4(func() (int, string, float64, error) {
		return 42, "foobar", 4.2, nil
	})
	ok2 := Try4(func() (int, string, float64, error) {
		return 42, "foobar", 4.2, errors.New("my error")
	})
	ok3 := Try4(func() (int, string, float64, error) {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry5() {
	ok1 := Try5(func() (int, string, float64, bool, error) {
		return 42, "foobar", 4.2, true, nil
	})
	ok2 := Try5(func() (int, string, float64, bool, error) {
		return 42, "foobar", 4.2, true, errors.New("my error")
	})
	ok3 := Try5(func() (int, string, float64, bool, error) {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTry6() {
	ok1 := Try6(func() (int, string, float64, bool, foo, error) {
		return 42, "foobar", 4.2, true, foo{}, nil
	})
	ok2 := Try6(func() (int, string, float64, bool, foo, error) {
		return 42, "foobar", 4.2, true, foo{}, errors.New("my error")
	})
	ok3 := Try6(func() (int, string, float64, bool, foo, error) {
		panic("my error")
	})

	fmt.Printf("%v\n", ok1)
	fmt.Printf("%v\n", ok2)
	fmt.Printf("%v\n", ok3)
	// Output:
	// true
	// false
	// false
}

func ExampleTryOr() {
	value1, ok1 := TryOr(func() (int, error) {
		return 42, nil
	}, 21)
	value2, ok2 := TryOr(func() (int, error) {
		return 42, errors.New("my error")
	}, 21)
	value3, ok3 := TryOr(func() (int, error) {
		panic("my error")
	}, 21)

	fmt.Printf("%v %v\n", value1, ok1)
	fmt.Printf("%v %v\n", value2, ok2)
	fmt.Printf("%v %v\n", value3, ok3)
	// Output:
	// 42 true
	// 21 false
	// 21 false
}

func ExampleTryOr1() {
	value1, ok1 := TryOr1(func() (int, error) {
		return 42, nil
	}, 21)
	value2, ok2 := TryOr1(func() (int, error) {
		return 42, errors.New("my error")
	}, 21)
	value3, ok3 := TryOr1(func() (int, error) {
		panic("my error")
	}, 21)

	fmt.Printf("%v %v\n", value1, ok1)
	fmt.Printf("%v %v\n", value2, ok2)
	fmt.Printf("%v %v\n", value3, ok3)
	// Output:
	// 42 true
	// 21 false
	// 21 false
}

func ExampleTryOr2() {
	value1, value2, ok3 := TryOr2(func() (int, string, error) {
		panic("my error")
	}, 21, "hello")

	fmt.Printf("%v %v %v\n", value1, value2, ok3)
	// Output: 21 hello false
}

func ExampleTryOr3() {
	value1, value2, value3, ok3 := TryOr3(func() (int, string, bool, error) {
		panic("my error")
	}, 21, "hello", false)

	fmt.Printf("%v %v %v %v\n", value1, value2, value3, ok3)
	// Output: 21 hello false false
}

func ExampleTryOr4() {
	value1, value2, value3, value4, ok3 := TryOr4(func() (int, string, bool, foo, error) {
		panic("my error")
	}, 21, "hello", false, foo{bar: "bar"})

	fmt.Printf("%v %v %v %v %v\n", value1, value2, value3, value4, ok3)
	// Output: 21 hello false {bar} false
}

func ExampleTryOr5() {
	value1, value2, value3, value4, value5, ok3 := TryOr5(func() (int, string, bool, foo, float64, error) {
		panic("my error")
	}, 21, "hello", false, foo{bar: "bar"}, 4.2)

	fmt.Printf("%v %v %v %v %v %v\n", value1, value2, value3, value4, value5, ok3)
	// Output: 21 hello false {bar} 4.2 false
}

func ExampleTryOr6() {
	value1, value2, value3, value4, value5, value6, ok3 := TryOr6(func() (int, string, bool, foo, float64, string, error) {
		panic("my error")
	}, 21, "hello", false, foo{bar: "bar"}, 4.2, "world")

	fmt.Printf("%v %v %v %v %v %v %v\n", value1, value2, value3, value4, value5, value6, ok3)
	// Output: 21 hello false {bar} 4.2 world false
}

func ExampleTryWithErrorValue() {
	err1, ok1 := TryWithErrorValue(func() error {
		return nil
	})
	err2, ok2 := TryWithErrorValue(func() error {
		return errors.New("my error")
	})
	err3, ok3 := TryWithErrorValue(func() error {
		panic("my error")
	})

	fmt.Printf("%v %v\n", err1, ok1)
	fmt.Printf("%v %v\n", err2, ok2)
	fmt.Printf("%v %v\n", err3, ok3)
	// Output:
	// <nil> true
	// my error false
	// my error false
}

func ExampleTryCatchWithErrorValue() {
	TryCatchWithErrorValue(
		func() error {
			panic("trigger an error")
		},
		func(err any) {
			fmt.Printf("catch: %s", err)
		},
	)

	// Output: catch: trigger an error
}

type myError struct{}

func (e myError) Error() string {
	return "my error"
}

func ExampleErrorsAs() {
	doSomething := func() error {
		return &myError{}
	}

	err := doSomething()

	if rateLimitErr, ok := ErrorsAs[*myError](err); ok {
		fmt.Printf("is type myError, err: %s", rateLimitErr.Error())
	} else {
		fmt.Printf("is not type myError")
	}

	// Output: is type myError, err: my error
}

func ExampleAssert() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
		}
	}()

	age := 20

	// won't panic
	Assert(age >= 18)

	// won't panic
	Assert(age >= 18, "age must be at least 18")

	// will panic
	Assert(age < 18)

	// will panic
	Assert(age < 18, "age must be less than 18")

	// Output: assertion failed
}

func ExampleAssertf() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
		}
	}()

	age := 20

	// won't panic
	Assertf(age >= 18, "age must be at least 18, got %d", age)

	// will panic
	Assertf(age < 18, "age must be less than 18, got %d", age)

	// Output: assertion failed: age must be less than 18, got 20
}

func ExampleIndexOf() {
	list := []string{"foo", "bar", "baz"}

	result := IndexOf(list, "bar")

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleIndexOf_notFound() {
	list := []string{"foo", "bar", "baz"}

	result := IndexOf(list, "qux")

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleLastIndexOf() {
	list := []string{"foo", "bar", "baz", "bar"}

	result := LastIndexOf(list, "bar")

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleLastIndexOf_notFound() {
	list := []string{"foo", "bar", "baz"}

	result := LastIndexOf(list, "qux")

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleFind() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result, found := Find(users, func(user User) bool {
		return user.Age > 30
	})

	fmt.Printf("%s %t", result.Name, found)
	// Output: Charlie true
}

func ExampleFind_notFound() {
	list := []int{1, 2, 3, 4, 5}

	result, found := Find(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleFindErr() {
	result, err := FindErr([]string{"a", "b", "c", "d"}, func(i string) (bool, error) {
		return i == "b", nil
	})
	fmt.Printf("%v %v\n", result, err)

	result, err = FindErr([]string{"foobar"}, func(i string) (bool, error) {
		return i == "b", nil
	})
	fmt.Printf("%v %v\n", result, err)

	result, err = FindErr([]string{"a", "b", "c"}, func(i string) (bool, error) {
		if i == "b" {
			return false, errors.New("b is not allowed")
		}
		return i == "b", nil
	})
	fmt.Printf("%v %v", result, err)
	// Output:
	// b <nil>
	//  <nil>
	//  b is not allowed
}

func ExampleFindIndexOf() {
	list := []int{1, 2, 3, 4, 5}

	result, index, found := FindIndexOf(list, func(n int) bool {
		return n > 2
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 3 2 true
}

func ExampleFindIndexOf_notFound() {
	list := []int{1, 2, 3, 4, 5}

	result, index, found := FindIndexOf(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 0 -1 false
}

func ExampleFindLastIndexOf() {
	list := []int{1, 2, 3, 4, 3, 5}

	result, index, found := FindLastIndexOf(list, func(n int) bool {
		return n == 3
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 3 4 true
}

func ExampleFindLastIndexOf_notFound() {
	list := []int{1, 2, 3, 4, 5}

	result, index, found := FindLastIndexOf(list, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d %d %t", result, index, found)
	// Output: 0 -1 false
}

func ExampleFindOrElse() {
	list := []int{1, 2, 3, 4, 5}

	result := FindOrElse(list, -1, func(n int) bool {
		return n > 10
	})

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleFindOrElse_found() {
	list := []int{1, 2, 3, 4, 5}

	result := FindOrElse(list, -1, func(n int) bool {
		return n > 3
	})

	fmt.Printf("%d", result)
	// Output: 4
}

func ExampleFindKey() {
	users := map[string]int{
		"Alice":   25,
		"Bob":     30,
		"Charlie": 35,
	}

	key, found := FindKey(users, 30)

	fmt.Printf("%s %t", key, found)
	// Output: Bob true
}

func ExampleFindKey_notFound() {
	users := map[string]int{
		"Alice":   25,
		"Bob":     30,
		"Charlie": 35,
	}

	key, found := FindKey(users, 40)

	fmt.Printf("%s %t", key, found)
	// Output:  false
}

func ExampleFindKeyBy() {
	users := map[string]int{
		"Alice":   25,
		"Bob":     30,
		"Charlie": 35,
	}

	key, found := FindKeyBy(users, func(name string, age int) bool {
		return age > 30
	})

	fmt.Printf("%s %t", key, found)
	// Output: Charlie true
}

func ExampleFindKeyBy_notFound() {
	users := map[string]int{
		"Alice":   25,
		"Bob":     30,
		"Charlie": 35,
	}

	key, found := FindKeyBy(users, func(name string, age int) bool {
		return age > 40
	})

	fmt.Printf("%s %t", key, found)
	// Output:  false
}

func ExampleFindUniques() {
	list := []int{1, 2, 2, 3, 3, 3, 4, 5}

	result := FindUniques(list)

	fmt.Printf("%v", result)
	// Output: [1 4 5]
}

func ExampleFindUniquesBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 25},
		{Name: "David", Age: 30},
		{Name: "Eve", Age: 35},
	}

	result := FindUniquesBy(users, func(user User) int {
		return user.Age
	})

	fmt.Printf("%d", len(result))
	// Output: 1
}

func ExampleFindDuplicates() {
	list := []int{1, 2, 2, 3, 3, 3, 4, 5}

	result := FindDuplicates(list)

	fmt.Printf("%v", result)
	// Output: [2 3]
}

func ExampleFindDuplicatesBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 25},
		{Name: "David", Age: 30},
		{Name: "Eve", Age: 35},
	}

	result := FindDuplicatesBy(users, func(user User) int {
		return user.Age
	})

	fmt.Printf("%d", len(result))
	// Output: 2
}

func ExampleFindDuplicatesByErr() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 25},
		{Name: "David", Age: 30},
		{Name: "Eve", Age: 35},
	}

	result, err := FindDuplicatesByErr(users, func(user User) (int, error) {
		if user.Name == "Charlie" {
			return 0, errors.New("charlie is not allowed")
		}
		return user.Age, nil
	})

	fmt.Printf("%d %v", len(result), err)
	// Output: 0 charlie is not allowed
}

func ExampleMin() {
	list := []int{3, 1, 4, 1, 5, 9, 2, 6}

	result := Min(list)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleMin_empty() {
	list := []int{}

	result := Min(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleMinIndex() {
	list := []int{3, 1, 4, 1, 5, 9, 2, 6}

	result, index := MinIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 1 1
}

func ExampleMinIndex_empty() {
	list := []int{}

	result, index := MinIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 0 -1
}

func ExampleMinBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result := MinBy(users, func(a, b User) bool {
		return a.Age < b.Age
	})

	fmt.Printf("%s", result.Name)
	// Output: Alice
}

func ExampleMinByErr() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result, err := MinByErr(users, func(a, b User) (bool, error) {
		if a.Name == "Bob" {
			return false, errors.New("bob is not allowed")
		}
		return a.Age < b.Age, nil
	})

	fmt.Printf("%v %v", result.Name, err)
	// Output:  bob is not allowed
}

func ExampleMinIndexBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result, index := MinIndexBy(users, func(a, b User) bool {
		return a.Age < b.Age
	})

	fmt.Printf("%s %d", result.Name, index)
	// Output: Alice 0
}

func ExampleMinIndexByErr() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result, _, err := MinIndexByErr(users, func(a, b User) (bool, error) {
		if a.Name == "Bob" {
			return false, errors.New("bob is not allowed")
		}
		return a.Age < b.Age, nil
	})

	fmt.Printf("%v %v", result.Name, err)
	// Output:  bob is not allowed
}

func ExampleEarliest() {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	result := Earliest(future, now, past)

	fmt.Printf("%t", result.Equal(past))
	// Output: true
}

func ExampleEarliestBy() {
	type Event struct {
		Name string
		Time time.Time
	}

	now := time.Now()
	events := []Event{
		{Name: "Event A", Time: now.Add(time.Hour)},
		{Name: "Event B", Time: now},
		{Name: "Event C", Time: now.Add(-time.Hour)},
	}

	result := EarliestBy(events, func(event Event) time.Time {
		return event.Time
	})

	fmt.Printf("%s", result.Name)
	// Output: Event C
}

func ExampleEarliestByErr() {
	type Event struct {
		Name string
		Time time.Time
	}

	now := time.Now()
	events := []Event{
		{Name: "Event A", Time: now.Add(time.Hour)},
		{Name: "Event B", Time: now},
		{Name: "Event C", Time: now.Add(-time.Hour)},
	}

	_, err := EarliestByErr(events, func(event Event) (time.Time, error) {
		if event.Name == "Event B" {
			return time.Time{}, errors.New("event b is not allowed")
		}
		return event.Time, nil
	})

	fmt.Printf("%v", err)
	// Output: event b is not allowed
}

func ExampleMax() {
	list := []int{3, 1, 4, 1, 5, 9, 2, 6}

	result := Max(list)

	fmt.Printf("%d", result)
	// Output: 9
}

func ExampleMax_empty() {
	list := []int{}

	result := Max(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleMaxIndex() {
	list := []int{3, 1, 4, 1, 5, 9, 2, 6}

	result, index := MaxIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 9 5
}

func ExampleMaxIndex_empty() {
	list := []int{}

	result, index := MaxIndex(list)

	fmt.Printf("%d %d", result, index)
	// Output: 0 -1
}

func ExampleMaxBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result := MaxBy(users, func(a, b User) bool {
		return a.Age > b.Age
	})

	fmt.Printf("%s", result.Name)
	// Output: Charlie
}

func ExampleMaxByErr() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	_, err := MaxByErr(users, func(a, b User) (bool, error) {
		if b.Name == "Bob" {
			return false, errors.New("bob is not allowed")
		}
		return a.Age > b.Age, nil
	})

	fmt.Printf("%v", err)
	// Output: bob is not allowed
}

func ExampleMaxIndexBy() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	result, index := MaxIndexBy(users, func(a, b User) bool {
		return a.Age > b.Age
	})

	fmt.Printf("%s %d", result.Name, index)
	// Output: Charlie 2
}

func ExampleMaxIndexByErr() {
	type User struct {
		Name string
		Age  int
	}

	users := []User{
		{Name: "Alice", Age: 25},
		{Name: "Bob", Age: 30},
		{Name: "Charlie", Age: 35},
	}

	_, _, err := MaxIndexByErr(users, func(a, b User) (bool, error) {
		if b.Name == "Bob" {
			return false, errors.New("bob is not allowed")
		}
		return a.Age > b.Age, nil
	})

	fmt.Printf("%v", err)
	// Output: bob is not allowed
}

func ExampleLatest() {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	result := Latest(future, now, past)

	fmt.Printf("%t", result.Equal(future))
	// Output: true
}

func ExampleLatestBy() {
	type Event struct {
		Name string
		Time time.Time
	}

	now := time.Now()
	events := []Event{
		{Name: "Event A", Time: now.Add(time.Hour)},
		{Name: "Event B", Time: now},
		{Name: "Event C", Time: now.Add(-time.Hour)},
	}

	result := LatestBy(events, func(event Event) time.Time {
		return event.Time
	})

	fmt.Printf("%s", result.Name)
	// Output: Event A
}

func ExampleLatestByErr() {
	type Event struct {
		Name string
		Time time.Time
		Err  error // Simulates error condition
	}

	now := time.Now()
	events := []Event{
		{Name: "Event A", Time: now.Add(time.Hour), Err: nil},
		{Name: "Event B", Time: now, Err: errors.New("event b error")},
		{Name: "Event C", Time: now.Add(-time.Hour), Err: nil},
	}

	_, err := LatestByErr(events, func(event Event) (time.Time, error) {
		if event.Err != nil {
			return time.Time{}, event.Err
		}
		return event.Time, nil
	})

	fmt.Printf("%v", err)
	// Output: event b error
}

func ExampleFirst() {
	list := []int{1, 2, 3, 4, 5}

	result, found := First(list)

	fmt.Printf("%d %t", result, found)
	// Output: 1 true
}

func ExampleFirst_empty() {
	list := []int{}

	result, found := First(list)

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleFirstOrEmpty() {
	list := []int{1, 2, 3, 4, 5}

	result := FirstOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleFirstOrEmpty_empty() {
	list := []int{}

	result := FirstOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleFirstOr() {
	list := []int{1, 2, 3, 4, 5}

	result := FirstOr(list, -1)

	fmt.Printf("%d", result)
	// Output: 1
}

func ExampleFirstOr_empty() {
	list := []int{}

	result := FirstOr(list, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleLast() {
	list := []int{1, 2, 3, 4, 5}

	result, found := Last(list)

	fmt.Printf("%d %t", result, found)
	// Output: 5 true
}

func ExampleLast_empty() {
	list := []int{}

	result, found := Last(list)

	fmt.Printf("%d %t", result, found)
	// Output: 0 false
}

func ExampleLastOrEmpty() {
	list := []int{1, 2, 3, 4, 5}

	result := LastOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 5
}

func ExampleLastOrEmpty_empty() {
	list := []int{}

	result := LastOrEmpty(list)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleLastOr() {
	list := []int{1, 2, 3, 4, 5}

	result := LastOr(list, -1)

	fmt.Printf("%d", result)
	// Output: 5
}

func ExampleLastOr_empty() {
	list := []int{}

	result := LastOr(list, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleNth() {
	list := []int{1, 2, 3, 4, 5}

	result, err := Nth(list, 2)

	fmt.Printf("%d %v", result, err)
	// Output: 3 <nil>
}

func ExampleNth_negative() {
	list := []int{1, 2, 3, 4, 5}

	result, err := Nth(list, -2)

	fmt.Printf("%d %v", result, err)
	// Output: 4 <nil>
}

func ExampleNth_outOfBounds() {
	list := []int{1, 2, 3, 4, 5}

	result, err := Nth(list, 10)

	fmt.Printf("%d %v", result, err)
	// Output: 0 nth: 10 out of slice bounds
}

func ExampleNthOr() {
	list := []int{1, 2, 3, 4, 5}

	result := NthOr(list, 2, -1)

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleNthOr_outOfBounds() {
	list := []int{1, 2, 3, 4, 5}

	result := NthOr(list, 10, -1)

	fmt.Printf("%d", result)
	// Output: -1
}

func ExampleNthOrEmpty() {
	list := []int{1, 2, 3, 4, 5}

	result := NthOrEmpty(list, 2)

	fmt.Printf("%d", result)
	// Output: 3
}

func ExampleNthOrEmpty_outOfBounds() {
	list := []int{1, 2, 3, 4, 5}

	result := NthOrEmpty(list, 10)

	fmt.Printf("%d", result)
	// Output: 0
}

func ExampleWithoutBy() {
	type User struct {
		ID   int
		Name string
	}
	// original users
	users := []User{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
	}

	// exclude users with IDs 2 and 3
	excludedIDs := []int{2, 3}

	// extract function to get the user ID
	extractID := func(user User) int {
		return user.ID
	}

	// filtering users
	filteredUsers := WithoutBy(users, extractID, excludedIDs...)

	// output the filtered users
	fmt.Printf("%v", filteredUsers)
	// Output:
	// [{1 Alice}]
}

func ExampleWithoutByErr() {
	type User struct {
		ID   int
		Name string
	}
	// original users
	users := []User{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
	}

	// exclude users with IDs 2 and 3
	excludedIDs := []int{2, 3}

	// extract function to get the user ID
	extractID := func(user User) (int, error) {
		if user.ID == 2 {
			return 0, errors.New("user 2 extraction failed")
		}
		return user.ID, nil
	}

	// filtering users
	filteredUsers, err := WithoutByErr(users, extractID, excludedIDs...)

	// output the filtered users
	fmt.Printf("%v %v", filteredUsers, err)
	// Output:
	// [] user 2 extraction failed
}

func ExampleKeys() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 3}

	result := Keys(kv, kv2)
	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [bar baz foo]
}

func ExampleUniqKeys() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"bar": 3}

	result := UniqKeys(kv, kv2)
	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [bar foo]
}

func ExampleValues() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 3}

	result := Values(kv, kv2)

	sort.Ints(result)
	fmt.Printf("%v", result)
	// Output: [1 2 3]
}

func ExampleUniqValues() {
	kv := map[string]int{"foo": 1, "bar": 2}
	kv2 := map[string]int{"baz": 2}

	result := UniqValues(kv, kv2)

	sort.Ints(result)
	fmt.Printf("%v", result)
	// Output: [1 2]
}

func ExampleValueOr() {
	kv := map[string]int{"foo": 1, "bar": 2}

	result1 := ValueOr(kv, "foo", 42)
	result2 := ValueOr(kv, "baz", 42)

	fmt.Printf("%v %v", result1, result2)
	// Output: 1 42
}

func ExamplePickBy() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := PickBy(kv, func(key string, value int) bool {
		return value%2 == 1
	})

	fmt.Printf("%v %v %v", len(result), result["foo"], result["baz"])
	// Output: 2 1 3
}

func ExamplePickByErr() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	_, err := PickByErr(kv, func(key string, value int) (bool, error) {
		if key == "bar" {
			return false, errors.New("key bar is not allowed")
		}
		return value%2 == 1, nil
	})

	fmt.Printf("%v", err)
	// Output: key bar is not allowed
}

func ExamplePickByKeys() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := PickByKeys(kv, []string{"foo", "baz"})

	fmt.Printf("%v %v %v", len(result), result["foo"], result["baz"])
	// Output: 2 1 3
}

func ExamplePickByValues() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := PickByValues(kv, []int{1, 3})

	fmt.Printf("%v %v %v", len(result), result["foo"], result["baz"])
	// Output: 2 1 3
}

func ExampleOmitBy() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := OmitBy(kv, func(key string, value int) bool {
		return value%2 == 1
	})

	fmt.Printf("%v", result)
	// Output: map[bar:2]
}

func ExampleOmitByErr() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	_, err := OmitByErr(kv, func(key string, value int) (bool, error) {
		if key == "bar" {
			return false, errors.New("key bar is not allowed")
		}
		return value%2 == 1, nil
	})

	fmt.Printf("%v", err)
	// Output: key bar is not allowed
}

func ExampleOmitByKeys() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := OmitByKeys(kv, []string{"foo", "baz"})

	fmt.Printf("%v", result)
	// Output: map[bar:2]
}

func ExampleOmitByValues() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := OmitByValues(kv, []int{1, 3})

	fmt.Printf("%v", result)
	// Output: map[bar:2]
}

func ExampleEntries() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := Entries(kv)

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].Key, result[j].Key) < 0
	})
	fmt.Printf("%v", result)
	// Output: [{bar 2} {baz 3} {foo 1}]
}

func ExampleFromEntries() {
	result := FromEntries([]Entry[string, int]{
		{
			Key:   "foo",
			Value: 1,
		},
		{
			Key:   "bar",
			Value: 2,
		},
		{
			Key:   "baz",
			Value: 3,
		},
	})

	fmt.Printf("%v %v %v %v", len(result), result["foo"], result["bar"], result["baz"])
	// Output: 3 1 2 3
}

func ExampleInvert() {
	kv := map[string]int{"foo": 1, "bar": 2, "baz": 3}

	result := Invert(kv)

	fmt.Printf("%v %v %v %v", len(result), result[1], result[2], result[3])
	// Output: 3 foo bar baz
}

func ExampleAssign() {
	result := Assign(
		map[string]int{"a": 1, "b": 2},
		map[string]int{"b": 3, "c": 4},
	)

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

	for i := range result {
		fmt.Printf("%d\n", len(result[i]))
	}
	// Output:
	// 3
	// 2
}

func ExampleMapKeys() {
	kv := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}

	result := MapKeys(kv, func(_, k int) string {
		return strconv.FormatInt(int64(k), 10)
	})

	fmt.Printf("%v %v %v %v %v", len(result), result["1"], result["2"], result["3"], result["4"])
	// Output: 4 1 2 3 4
}

func ExampleMapKeysErr() {
	kv := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}

	_, err := MapKeysErr(kv, func(_, k int) (string, error) {
		if k == 3 {
			return "", errors.New("key 3 is not allowed")
		}
		return strconv.FormatInt(int64(k), 10), nil
	})

	fmt.Printf("%v", err)
	// Output: key 3 is not allowed
}

func ExampleMapValues() {
	kv := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}

	result := MapValues(kv, func(v, _ int) string {
		return strconv.FormatInt(int64(v), 10)
	})

	fmt.Printf("%v %q %q %q %q", len(result), result[1], result[2], result[3], result[4])
	// Output: 4 "1" "2" "3" "4"
}

func ExampleMapValuesErr() {
	kv := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}

	_, err := MapValuesErr(kv, func(v, _ int) (string, error) {
		if v == 3 {
			return "", errors.New("value 3 is not allowed")
		}
		return strconv.FormatInt(int64(v), 10), nil
	})

	fmt.Printf("%v", err)
	// Output: value 3 is not allowed
}

func ExampleMapEntries() {
	kv := map[string]int{"foo": 1, "bar": 2}

	result := MapEntries(kv, func(k string, v int) (int, string) {
		return v, k
	})

	fmt.Printf("%v", result)
	// Output: map[1:foo 2:bar]
}

func ExampleMapEntriesErr() {
	kv := map[string]int{"foo": 1, "bar": 2}

	_, err := MapEntriesErr(kv, func(k string, v int) (int, string, error) {
		if k == "foo" {
			return 0, "", errors.New("entry foo is not allowed")
		}
		return v, k, nil
	})

	fmt.Printf("%v", err)
	// Output: entry foo is not allowed
}

func ExampleMapToSlice() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	result := MapToSlice(kv, func(k int, v int64) string {
		return fmt.Sprintf("%d_%d", k, v)
	})

	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [1_1 2_2 3_3 4_4]
}

func ExampleMapToSliceErr() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	_, err := MapToSliceErr(kv, func(k int, v int64) (string, error) {
		if k == 3 {
			return "", errors.New("key 3 is not allowed")
		}
		return fmt.Sprintf("%d_%d", k, v), nil
	})

	fmt.Printf("%v", err)
	// Output: key 3 is not allowed
}

func ExampleFilterMapToSlice() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	result := FilterMapToSlice(kv, func(k int, v int64) (string, bool) {
		return fmt.Sprintf("%d_%d", k, v), k%2 == 0
	})

	sort.Strings(result)
	fmt.Printf("%v", result)
	// Output: [2_2 4_4]
}

func ExampleFilterMapToSliceErr() {
	kv := map[int]int64{1: 1, 2: 2, 3: 3, 4: 4}

	_, err := FilterMapToSliceErr(kv, func(k int, v int64) (string, bool, error) {
		if k == 3 {
			return "", false, errors.New("key 3 is not allowed")
		}
		return fmt.Sprintf("%d_%d", k, v), k%2 == 0, nil
	})

	fmt.Printf("%v", err)
	// Output: key 3 is not allowed
}

func ExampleFilterKeys() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result := FilterKeys(kv, func(k int, v string) bool {
		return v == "foo"
	})

	fmt.Printf("%v", result)
	// Output: [1]
}

func ExampleFilterValues() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result := FilterValues(kv, func(k int, v string) bool {
		return v == "foo"
	})

	fmt.Printf("%v", result)
	// Output: [foo]
}

func ExampleFilterKeysErr() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result, err := FilterKeysErr(kv, func(k int, v string) (bool, error) {
		if k == 3 {
			return false, errors.New("key 3 not allowed")
		}
		return v == "foo", nil
	})
	fmt.Printf("%v, %v\n", result, err)

	result, err = FilterKeysErr(kv, func(k int, v string) (bool, error) {
		return v == "bar", nil
	})
	fmt.Printf("%v, %v\n", result, err)
	// Output:
	// [], key 3 not allowed
	// [2], <nil>
}

func ExampleFilterValuesErr() {
	kv := map[int]string{1: "foo", 2: "bar", 3: "baz"}

	result, err := FilterValuesErr(kv, func(k int, v string) (bool, error) {
		if k == 3 {
			return false, errors.New("key 3 not allowed")
		}
		return v == "foo", nil
	})
	fmt.Printf("%v, %v\n", result, err)

	result, err = FilterValuesErr(kv, func(k int, v string) (bool, error) {
		return v == "bar", nil
	})
	fmt.Printf("%v, %v\n", result, err)
	// Output:
	// [], key 3 not allowed
	// [bar], <nil>
}

func ExampleRange() {
	result1 := Range(4)
	result2 := Range(-4)
	result3 := RangeFrom(1, 5)
	result4 := RangeFrom(1.0, 5)
	result5 := RangeWithSteps(0, 20, 5)
	result6 := RangeWithSteps[float32](-1.0, -4.0, -1.0)
	result7 := RangeWithSteps(1, 4, -1)
	result8 := Range(0)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	fmt.Printf("%v\n", result6)
	fmt.Printf("%v\n", result7)
	fmt.Printf("%v\n", result8)
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

func ExampleClamp() {
	result1 := Clamp(0, -10, 10)
	result2 := Clamp(-42, -10, 10)
	result3 := Clamp(42, -10, 10)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	// Output:
	// 0
	// -10
	// 10
}

func ExampleSum() {
	list := []int{1, 2, 3, 4, 5}

	sum := Sum(list)

	fmt.Printf("%v", sum)
	// Output: 15
}

func ExampleSumBy() {
	list := []string{"foo", "bar"}

	result := SumBy(list, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 6
}

func ExampleSumByErr() {
	list := []string{"foo", "bar", "baz"}

	_, err := SumByErr(list, func(item string) (int, error) {
		if item == "bar" {
			return 0, errors.New("bar is not allowed")
		}
		return len(item), nil
	})

	fmt.Printf("%v", err)
	// Output: bar is not allowed
}

func ExampleProduct() {
	list := []int{1, 2, 3, 4, 5}

	result := Product(list)

	fmt.Printf("%v", result)
	// Output: 120
}

func ExampleProductBy() {
	list := []string{"foo", "bar"}

	result := ProductBy(list, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 9
}

func ExampleProductByErr() {
	list := []string{"foo", "bar", "baz"}

	_, err := ProductByErr(list, func(item string) (int, error) {
		if item == "bar" {
			return 0, errors.New("bar is not allowed")
		}
		return len(item), nil
	})

	fmt.Printf("%v", err)
	// Output: bar is not allowed
}

func ExampleMean() {
	list := []int{1, 2, 3, 4, 5}

	result := Mean(list)

	fmt.Printf("%v", result)
	// Output: 3
}

func ExampleMeanBy() {
	list := []string{"foo", "bar"}

	result := MeanBy(list, func(item string) int {
		return len(item)
	})

	fmt.Printf("%v", result)
	// Output: 3
}

func ExampleMeanByErr() {
	list := []string{"foo", "bar", "baz"}

	_, err := MeanByErr(list, func(item string) (int, error) {
		if item == "bar" {
			return 0, errors.New("bar is not allowed")
		}
		return len(item), nil
	})

	fmt.Printf("%v", err)
	// Output: bar is not allowed
}

func ExampleFilter() {
	list := []int64{1, 2, 3, 4}

	result := Filter(list, func(nbr int64, index int) bool {
		return nbr%2 == 0
	})

	fmt.Printf("%v", result)
	// Output: [2 4]
}

func ExampleFilterErr() {
	list := []int64{1, 2, 3, 4}

	result, err := FilterErr(list, func(nbr int64, index int) (bool, error) {
		if nbr == 3 {
			return false, errors.New("number 3 is not allowed")
		}
		return nbr%2 == 0, nil
	})
	fmt.Printf("%v, %v\n", result, err)

	result, err = FilterErr([]int64{1, 2, 4, 6}, func(nbr int64, index int) (bool, error) {
		return nbr%2 == 0, nil
	})
	fmt.Printf("%v, %v\n", result, err)
	// Output:
	// [], number 3 is not allowed
	// [2 4 6], <nil>
}

func ExampleMap() {
	list := []int64{1, 2, 3, 4}

	result := Map(list, func(nbr int64, index int) string {
		return strconv.FormatInt(nbr*2, 10)
	})

	fmt.Printf("%v", result)
	// Output: [2 4 6 8]
}

func ExampleMapErr() {
	list := []int64{1, 2, 3, 4}

	_, err := MapErr(list, func(nbr int64, index int) (string, error) {
		if nbr == 3 {
			return "", errors.New("number 3 is not allowed")
		}
		return strconv.FormatInt(nbr*2, 10), nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleUniqMap() {
	type User struct {
		Name string
		Age  int
	}
	users := []User{{Name: "Alex", Age: 10}, {Name: "Alex", Age: 12}, {Name: "Bob", Age: 11}, {Name: "Alice", Age: 20}}

	result := UniqMap(users, func(u User, index int) string {
		return u.Name
	})

	sort.Strings(result)

	fmt.Printf("%v", result)
	// Output: [Alex Alice Bob]
}

func ExampleFilterMap() {
	list := []int64{1, 2, 3, 4}

	result := FilterMap(list, func(nbr int64, index int) (string, bool) {
		return strconv.FormatInt(nbr*2, 10), nbr%2 == 0
	})

	fmt.Printf("%v", result)
	// Output: [4 8]
}

func ExampleFlatMap() {
	list := []int64{1, 2, 3, 4}

	result := FlatMap(list, func(nbr int64, index int) []string {
		return []string{
			strconv.FormatInt(nbr, 10), // base 10
			strconv.FormatInt(nbr, 2),  // base 2
		}
	})

	fmt.Printf("%v", result)
	// Output: [1 1 2 10 3 11 4 100]
}

func ExampleFlatMapErr() {
	list := []int64{1, 2, 3, 4}

	_, err := FlatMapErr(list, func(nbr int64, index int) ([]string, error) {
		if nbr == 3 {
			return nil, errors.New("number 3 is not allowed")
		}
		return []string{
			strconv.FormatInt(nbr, 10), // base 10
			strconv.FormatInt(nbr, 2),  // base 2
		}, nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleReduce() {
	list := []int64{1, 2, 3, 4}

	result := Reduce(list, func(agg, item int64, index int) int64 {
		return agg + item
	}, 0)

	fmt.Printf("%v", result)
	// Output: 10
}

func ExampleReduceErr() {
	list := []int64{1, 2, 3, 4}

	_, err := ReduceErr(list, func(agg, item int64, index int) (int64, error) {
		if item == 3 {
			return 0, errors.New("number 3 is not allowed")
		}
		return agg + item, nil
	}, 0)

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleReduceRight() {
	list := [][]int{{0, 1}, {2, 3}, {4, 5}}

	result := ReduceRight(list, func(agg, item []int, index int) []int {
		return append(agg, item...)
	}, []int{})

	fmt.Printf("%v", result)
	// Output: [4 5 2 3 0 1]
}

func ExampleReduceRightErr() {
	list := [][]int{{0, 1}, {2, 3}, {4, 5}}

	_, err := ReduceRightErr(list, func(agg, item []int, index int) ([]int, error) {
		if index == 0 {
			return nil, errors.New("index 0 is not allowed")
		}
		return append(agg, item...), nil
	}, []int{})

	fmt.Printf("%v", err)
	// Output: index 0 is not allowed
}

func ExampleForEach() {
	list := []int64{1, 2, 3, 4}

	ForEach(list, func(x int64, _ int) {
		fmt.Println(x)
	})

	// Output:
	// 1
	// 2
	// 3
	// 4
}

func ExampleForEachWhile() {
	list := []int64{1, 2, -math.MaxInt, 4}

	ForEachWhile(list, func(x int64, _ int) bool {
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

	fmt.Printf("%v", result)
	// Output: [0 1 2]
}

func ExampleUniq() {
	list := []int{1, 2, 2, 1}

	result := Uniq(list)

	fmt.Printf("%v", result)
	// Output: [1 2]
}

func ExampleUniqBy() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := UniqBy(list, func(i int) int {
		return i % 3
	})

	fmt.Printf("%v", result)
	// Output: [0 1 2]
}

func ExampleUniqByErr() {
	list := []int{0, 1, 2, 3, 4, 5}

	_, err := UniqByErr(list, func(i int) (int, error) {
		if i == 3 {
			return 0, errors.New("number 3 is not allowed")
		}
		return i % 3, nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleGroupBy() {
	list := []int{0, 1, 2, 3, 4, 5}

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

func ExampleGroupByErr() {
	list := []int{0, 1, 2, 3, 4, 5}

	_, err := GroupByErr(list, func(i int) (int, error) {
		if i == 3 {
			return 0, errors.New("number 3 is not allowed")
		}
		return i % 3, nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleGroupByMap() {
	list := []int{0, 1, 2, 3, 4, 5}

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

func ExampleGroupByMapErr() {
	list := []int{0, 1, 2, 3, 4, 5}

	_, err := GroupByMapErr(list, func(i int) (int, int, error) {
		if i == 3 {
			return 0, 0, errors.New("number 3 is not allowed")
		}
		return i % 3, i * 2, nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleChunk() {
	list := []int{0, 1, 2, 3, 4}

	result := Chunk(list, 2)

	for _, item := range result {
		fmt.Printf("%v\n", item)
	}
	// Output:
	// [0 1]
	// [2 3]
	// [4]
}

func ExampleWindow() {
	list := []int{1, 2, 3, 4, 5}

	result := Window(list, 3)

	for _, item := range result {
		fmt.Printf("%v\n", item)
	}
	// Output:
	// [1 2 3]
	// [2 3 4]
	// [3 4 5]
}

func ExampleSliding() {
	list := []int{1, 2, 3, 4, 5, 6, 7, 8}

	// Overlapping windows (step < size)
	result1 := Sliding(list, 3, 1)
	fmt.Printf("Overlapping: %v\n", result1)

	// Non-overlapping windows (step == size, like Chunk)
	result2 := Sliding(list, 3, 3)
	fmt.Printf("Non-overlapping: %v\n", result2)

	// Step > size (skipping elements)
	result3 := Sliding(list, 2, 3)
	fmt.Printf("With step: %v\n", result3)

	// Output:
	// Overlapping: [[1 2 3] [2 3 4] [3 4 5] [4 5 6] [5 6 7] [6 7 8]]
	// Non-overlapping: [[1 2 3] [4 5 6]]
	// With step: [[1 2] [4 5] [7 8]]
}

func ExamplePartitionBy() {
	list := []int{-2, -1, 0, 1, 2, 3, 4}

	result := PartitionBy(list, func(x int) string {
		if x < 0 {
			return "negative"
		} else if x%2 == 0 {
			return "even"
		}
		return "odd"
	})

	for _, item := range result {
		fmt.Printf("%v\n", item)
	}
	// Output:
	// [-2 -1]
	// [0 2 4]
	// [1 3]
}

func ExamplePartitionByErr() {
	list := []int{-2, -1, 0, 1, 2, 3, 4}

	_, err := PartitionByErr(list, func(x int) (string, error) {
		if x == 0 {
			return "", errors.New("zero is not allowed")
		}
		if x < 0 {
			return "negative", nil
		} else if x%2 == 0 {
			return "even", nil
		}
		return "odd", nil
	})

	fmt.Printf("%v", err)
	// Output: zero is not allowed
}

func ExampleFlatten() {
	list := [][]int{{0, 1, 2}, {3, 4, 5}}

	result := Flatten(list)

	fmt.Printf("%v", result)
	// Output: [0 1 2 3 4 5]
}

func ExampleConcat() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Concat(list, list)

	fmt.Printf("%v", result)
	// Output: [0 1 2 3 4 5 0 1 2 3 4 5]
}

func ExampleInterleave() {
	list1 := [][]int{{1, 4, 7}, {2, 5, 8}, {3, 6, 9}}
	list2 := [][]int{{1}, {2, 5, 8}, {3, 6}, {4, 7, 9, 10}}

	result1 := Interleave(list1...)
	result2 := Interleave(list2...)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	// Output:
	// [1 2 3 4 5 6 7 8 9]
	// [1 2 3 4 5 6 7 8 9 10]
}

func ExampleShuffle() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Shuffle(list)

	fmt.Printf("%v", result)
}

func ExampleReverse() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Reverse(list)

	fmt.Printf("%v", result)
	// Output: [5 4 3 2 1 0]
}

func ExampleFill() {
	list := []foo{{"a"}, {"a"}}

	result := Fill(list, foo{"b"})

	fmt.Printf("%v", result)
	// Output: [{b} {b}]
}

func ExampleRepeat() {
	result := Repeat(2, foo{"a"})

	fmt.Printf("%v", result)
	// Output: [{a} {a}]
}

func ExampleRepeatBy() {
	result := RepeatBy(5, func(i int) string {
		return strconv.FormatInt(int64(math.Pow(float64(i), 2)), 10)
	})

	fmt.Printf("%v", result)
	// Output: [0 1 4 9 16]
}

func ExampleRepeatByErr() {
	result, err := RepeatByErr(5, func(i int) (string, error) {
		if i == 3 {
			return "", errors.New("index 3 is not allowed")
		}
		return fmt.Sprintf("item-%d", i), nil
	})

	fmt.Printf("%d %v", len(result), err)
	// Output: 0 index 3 is not allowed
}

func ExampleKeyBy() {
	list := []string{"a", "aa", "aaa"}

	result := KeyBy(list, func(str string) int {
		return len(str)
	})

	fmt.Printf("%v", result)
	// Output: map[1:a 2:aa 3:aaa]
}

func ExampleKeyByErr() {
	list := []string{"a", "aa", "aaa"}

	_, err := KeyByErr(list, func(str string) (int, error) {
		if str == "aa" {
			return 0, errors.New("aa is not allowed")
		}
		return len(str), nil
	})

	fmt.Printf("%v", err)
	// Output: aa is not allowed
}

func ExampleSliceToMap() {
	list := []string{"a", "aa", "aaa"}

	result := SliceToMap(list, func(str string) (string, int) {
		return str, len(str)
	})

	fmt.Printf("%v", result)
	// Output: map[a:1 aa:2 aaa:3]
}

func ExampleFilterSliceToMap() {
	list := []string{"a", "aa", "aaa"}

	result := FilterSliceToMap(list, func(str string) (string, int, bool) {
		return str, len(str), len(str) > 1
	})

	fmt.Printf("%v", result)
	// Output: map[aa:2 aaa:3]
}

func ExampleKeyify() {
	list := []string{"a", "a", "b", "b", "d"}

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
	list := []int{0, 1, 2, 3, 4, 5}

	result := Drop(list, 2)

	fmt.Printf("%v", result)
	// Output: [2 3 4 5]
}

func ExampleDropRight() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := DropRight(list, 2)

	fmt.Printf("%v", result)
	// Output: [0 1 2 3]
}

func ExampleDropWhile() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := DropWhile(list, func(val int) bool {
		return val < 2
	})

	fmt.Printf("%v", result)
	// Output: [2 3 4 5]
}

func ExampleDropRightWhile() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := DropRightWhile(list, func(val int) bool {
		return val > 2
	})

	fmt.Printf("%v", result)
	// Output: [0 1 2]
}

func ExampleTake() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Take(list, 3)

	fmt.Printf("%v", result)
	// Output: [0 1 2]
}

func ExampleTakeWhile() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := TakeWhile(list, func(val int) bool {
		return val < 3
	})

	fmt.Printf("%v", result)
	// Output: [0 1 2]
}

func ExampleTakeFilter() {
	list := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	result := TakeFilter(
		list, 3, func(val, index int) bool {
			return val%2 == 0
		},
	)

	fmt.Printf("%v", result)
	// Output: [2 4 6]
}

func ExampleDropByIndex() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := DropByIndex(list, 2)

	fmt.Printf("%v", result)
	// Output: [0 1 3 4 5]
}

func ExampleReject() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Reject(list, func(x, _ int) bool {
		return x%2 == 0
	})

	fmt.Printf("%v", result)
	// Output: [1 3 5]
}

func ExampleRejectErr() {
	list := []int64{0, 1, 2, 3, 4, 5}

	result, err := RejectErr(list, func(x int64, index int) (bool, error) {
		if x == 3 {
			return false, errors.New("number 3 is not allowed")
		}
		return x%2 == 0, nil
	})
	fmt.Printf("%v, %v\n", result, err)

	result, err = RejectErr([]int64{0, 1, 2, 4, 6}, func(x int64, index int) (bool, error) {
		return x%2 == 0, nil
	})
	fmt.Printf("%v, %v\n", result, err)
	// Output:
	// [], number 3 is not allowed
	// [1], <nil>
}

func ExampleCount() {
	list := []int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3}

	result := Count(list, 2)

	fmt.Printf("%v", result)
	// Output: 2
}

func ExampleCountBy() {
	list := []int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3}

	result := CountBy(list, func(i int) bool {
		return i < 4
	})

	fmt.Printf("%v", result)
	// Output: 8
}

func ExampleCountByErr() {
	list := []int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3}

	_, err := CountByErr(list, func(i int) (bool, error) {
		if i == 3 {
			return false, errors.New("number 3 is not allowed")
		}
		return i < 4, nil
	})

	fmt.Printf("%v", err)
	// Output: number 3 is not allowed
}

func ExampleCountValues() {
	result1 := CountValues([]int{})
	result2 := CountValues([]int{1, 2})
	result3 := CountValues([]int{1, 2, 2})
	result4 := CountValues([]string{"foo", "bar", ""})
	result5 := CountValues([]string{"foo", "bar", "bar"})

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

	result1 := CountValuesBy([]int{}, isEven)
	result2 := CountValuesBy([]int{1, 2}, isEven)
	result3 := CountValuesBy([]int{1, 2, 2}, isEven)

	length := func(v string) int {
		return len(v)
	}

	result4 := CountValuesBy([]string{"foo", "bar", ""}, length)
	result5 := CountValuesBy([]string{"foo", "bar", "bar"}, length)

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
	list := []int{0, 1, 2, 3, 4, 5}

	result := Subset(list, 2, 3)

	fmt.Printf("%v", result)
	// Output: [2 3 4]
}

func ExampleSlice() {
	list := []int{0, 1, 2, 3, 4, 5}

	result := Slice(list, 1, 4)
	fmt.Printf("%v\n", result)

	result = Slice(list, 4, 1)
	fmt.Printf("%v\n", result)

	result = Slice(list, 4, 5)
	fmt.Printf("%v\n", result)

	// Output:
	// [1 2 3]
	// []
	// [4]
}

func ExampleReplace() {
	list := []int{0, 1, 0, 1, 2, 3, 0}

	result := Replace(list, 0, 42, 1)
	fmt.Printf("%v\n", result)

	result = Replace(list, -1, 42, 1)
	fmt.Printf("%v\n", result)

	result = Replace(list, 0, 42, 2)
	fmt.Printf("%v\n", result)

	result = Replace(list, 0, 42, -1)
	fmt.Printf("%v\n", result)

	// Output:
	// [42 1 0 1 2 3 0]
	// [0 1 0 1 2 3 0]
	// [42 1 42 1 2 3 0]
	// [42 1 42 1 2 3 42]
}

func ExampleClone() {
	input := []int{1, 2, 3, 4, 5}

	output := Clone(input)
	input[0] = 42

	fmt.Printf("%v", output)
	// Output: [1 2 3 4 5]
}

func ExampleCompact() {
	list := []string{"", "foo", "", "bar", ""}

	result := Compact(list)

	fmt.Printf("%v", result)

	// Output: [foo bar]
}

func ExampleIsSorted() {
	list := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	result := IsSorted(list)

	fmt.Printf("%v", result)

	// Output: true
}

func ExampleIsSortedBy() {
	list := []string{"a", "bb", "ccc"}

	result := IsSortedBy(list, func(s string) int {
		return len(s)
	})

	fmt.Printf("%v", result)

	// Output: true
}

func ExampleCut() {
	collection := []string{"a", "b", "c", "d", "e", "f", "g"}

	// Test with valid separator
	before, after, found := Cut(collection, []string{"b", "c", "d"})
	fmt.Printf("Before: %v, After: %v, Found: %t\n", before, after, found)

	// Test with separator not found
	before2, after2, found2 := Cut(collection, []string{"z"})
	fmt.Printf("Before: %v, After: %v, Found: %t\n", before2, after2, found2)

	// Test with separator at beginning
	before3, after3, found3 := Cut(collection, []string{"a", "b"})
	fmt.Printf("Before: %v, After: %v, Found: %t\n", before3, after3, found3)

	// Output:
	// Before: [a], After: [e f g], Found: true
	// Before: [a b c d e f g], After: [], Found: false
	// Before: [], After: [c d e f g], Found: true
}

func ExampleCutPrefix() {
	collection := []string{"a", "b", "c", "d", "e", "f", "g"}

	// Test with valid prefix
	after, found := CutPrefix(collection, []string{"a", "b", "c"})
	fmt.Printf("After: %v, Found: %t\n", after, found)

	// Test with prefix not found
	after2, found2 := CutPrefix(collection, []string{"b"})
	fmt.Printf("After: %v, Found: %t\n", after2, found2)

	// Test with empty prefix
	after3, found3 := CutPrefix(collection, []string{})
	fmt.Printf("After: %v, Found: %t\n", after3, found3)

	// Output:
	// After: [d e f g], Found: true
	// After: [a b c d e f g], Found: false
	// After: [a b c d e f g], Found: true
}

func ExampleCutSuffix() {
	collection := []string{"a", "b", "c", "d", "e", "f", "g"}

	// Test with valid suffix
	before, found := CutSuffix(collection, []string{"f", "g"})
	fmt.Printf("Before: %v, Found: %t\n", before, found)

	// Test with suffix not found
	before2, found2 := CutSuffix(collection, []string{"b"})
	fmt.Printf("Before: %v, Found: %t\n", before2, found2)

	// Test with empty suffix
	before3, found3 := CutSuffix(collection, []string{})
	fmt.Printf("Before: %v, Found: %t\n", before3, found3)

	// Output:
	// Before: [a b c d e], Found: true
	// Before: [a b c d e f g], Found: false
	// Before: [a b c d e f g], Found: true
}

func ExampleTrim() {
	collection := []int{0, 1, 2, 0, 3, 0}

	// Test with valid cutset
	result := Trim(collection, []int{0})
	fmt.Printf("Trim with cutset {0}: %v\n", result)

	// Test with string collection
	words := []string{"  hello  ", "world", "  "}
	result2 := Trim(words, []string{" "})
	fmt.Printf("Trim with string cutset: %v\n", result2)

	// Test with no cutset elements
	result3 := Trim(collection, []int{5})
	fmt.Printf("Trim with cutset {5} (not present): %v\n", result3)

	// Output:
	// Trim with cutset {0}: [1 2 0 3]
	// Trim with string cutset: [  hello   world   ]
	// Trim with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimLeft() {
	collection := []int{0, 1, 2, 0, 3, 0}

	// Test with valid cutset
	result := TrimLeft(collection, []int{0})
	fmt.Printf("TrimLeft with cutset {0}: %v\n", result)

	// Test with string collection
	words := []string{"  hello  ", "world", "  "}
	result2 := TrimLeft(words, []string{" "})
	fmt.Printf("TrimLeft with string cutset: %v\n", result2)

	// Test with no cutset elements
	result3 := TrimLeft(collection, []int{5})
	fmt.Printf("TrimLeft with cutset {5} (not present): %v\n", result3)

	// Output:
	// TrimLeft with cutset {0}: [1 2 0 3 0]
	// TrimLeft with string cutset: [  hello   world   ]
	// TrimLeft with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimPrefix() {
	collection := []int{1, 2, 1, 2, 3}

	// Test with valid prefix
	result := TrimPrefix(collection, []int{1, 2})
	fmt.Printf("TrimPrefix with prefix {1,2}: %v\n", result)

	// Test with string collection
	words := []string{"hello", "hello", "world"}
	result2 := TrimPrefix(words, []string{"hello"})
	fmt.Printf("TrimPrefix with string prefix: %v\n", result2)

	// Test with prefix not present
	result3 := TrimPrefix(collection, []int{5, 6})
	fmt.Printf("TrimPrefix with prefix {5,6} (not present): %v\n", result3)

	// Output:
	// TrimPrefix with prefix {1,2}: [3]
	// TrimPrefix with string prefix: [world]
	// TrimPrefix with prefix {5,6} (not present): [1 2 1 2 3]
}

func ExampleTrimRight() {
	collection := []int{0, 1, 2, 0, 3, 0}

	// Test with valid cutset
	result := TrimRight(collection, []int{0})
	fmt.Printf("TrimRight with cutset {0}: %v\n", result)

	// Test with string collection
	words := []string{"  hello  ", "world", "  "}
	result2 := TrimRight(words, []string{" "})
	fmt.Printf("TrimRight with string cutset: %v\n", result2)

	// Test with no cutset elements
	result3 := TrimRight(collection, []int{5})
	fmt.Printf("TrimRight with cutset {5} (not present): %v\n", result3)

	// Output:
	// TrimRight with cutset {0}: [0 1 2 0 3]
	// TrimRight with string cutset: [  hello   world   ]
	// TrimRight with cutset {5} (not present): [0 1 2 0 3 0]
}

func ExampleTrimSuffix() {
	collection := []int{1, 2, 1, 2, 3}

	// Test with valid suffix
	result := TrimSuffix(collection, []int{1, 2})
	fmt.Printf("TrimSuffix with suffix {1,2}: %v\n", result)

	// Test with string collection
	words := []string{"hello", "world", "test"}
	result2 := TrimSuffix(words, []string{"test"})
	fmt.Printf("TrimSuffix with string suffix: %v\n", result2)

	// Test with suffix not present
	result3 := TrimSuffix(collection, []int{5, 6})
	fmt.Printf("TrimSuffix with suffix {5,6} (not present): %v\n", result3)

	// Output:
	// TrimSuffix with suffix {1,2}: [1 2 1 2 3]
	// TrimSuffix with string suffix: [hello world]
	// TrimSuffix with suffix {5,6} (not present): [1 2 1 2 3]
}

func ExampleSubstring() {
	result1 := Substring("hello", 2, 3)
	result2 := Substring("hello", -4, 3)
	result3 := Substring("hello", -2, math.MaxUint)
	result4 := Substring("🏠🐶🐱", 0, 2)
	result5 := Substring("你好，世界", 0, 3)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	fmt.Printf("%v\n", result5)
	// Output:
	// llo
	// ell
	// lo
	// 🏠🐶
	// 你好，
}

func ExampleChunkString() {
	result1 := ChunkString("123456", 2)
	result2 := ChunkString("1234567", 2)
	result3 := ChunkString("", 2)
	result4 := ChunkString("1", 2)

	fmt.Printf("%v\n", result1)
	fmt.Printf("%v\n", result2)
	fmt.Printf("%v\n", result3)
	fmt.Printf("%v\n", result4)
	// Output:
	// [12 34 56]
	// [12 34 56 7]
	// []
	// [1]
}

func ExampleRuneLength() {
	result1, chars1 := RuneLength("hellô"), len("hellô")
	result2, chars2 := RuneLength("🤘"), len("🤘")

	fmt.Printf("%v %v\n", result1, chars1)
	fmt.Printf("%v %v\n", result2, chars2)
	// Output:
	// 5 6
	// 1 4
}

func ExampleT2() {
	result := T2("hello", 2)
	fmt.Printf("%v %v", result.A, result.B)
	// Output: hello 2
}

func ExampleT3() {
	result := T3("hello", 2, true)
	fmt.Printf("%v %v %v", result.A, result.B, result.C)
	// Output: hello 2 true
}

func ExampleT4() {
	result := T4("hello", 2, true, foo{bar: "bar"})
	fmt.Printf("%v %v %v %v", result.A, result.B, result.C, result.D)
	// Output: hello 2 true {bar}
}

func ExampleT5() {
	result := T5("hello", 2, true, foo{bar: "bar"}, 4.2)
	fmt.Printf("%v %v %v %v %v", result.A, result.B, result.C, result.D, result.E)
	// Output: hello 2 true {bar} 4.2
}

func ExampleT6() {
	result := T6("hello", 2, true, foo{bar: "bar"}, 4.2, "plop")
	fmt.Printf("%v %v %v %v %v %v", result.A, result.B, result.C, result.D, result.E, result.F)
	// Output: hello 2 true {bar} 4.2 plop
}

func ExampleT7() {
	result := T7("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false)
	fmt.Printf("%v %v %v %v %v %v %v", result.A, result.B, result.C, result.D, result.E, result.F, result.G)
	// Output: hello 2 true {bar} 4.2 plop false
}

func ExampleT8() {
	result := T8("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42)
	fmt.Printf("%v %v %v %v %v %v %v %v", result.A, result.B, result.C, result.D, result.E, result.F, result.G, result.H)
	// Output: hello 2 true {bar} 4.2 plop false 42
}

func ExampleT9() {
	result := T9("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42, "hello world")
	fmt.Printf("%v %v %v %v %v %v %v %v %v", result.A, result.B, result.C, result.D, result.E, result.F, result.G, result.H, result.I)
	// Output: hello 2 true {bar} 4.2 plop false 42 hello world
}

func ExampleUnpack2() {
	a, b := Unpack2(T2("hello", 2))
	fmt.Printf("%v %v", a, b)
	// Output: hello 2
}

func ExampleUnpack3() {
	a, b, c := Unpack3(T3("hello", 2, true))
	fmt.Printf("%v %v %v", a, b, c)
	// Output: hello 2 true
}

func ExampleUnpack4() {
	a, b, c, d := Unpack4(T4("hello", 2, true, foo{bar: "bar"}))
	fmt.Printf("%v %v %v %v", a, b, c, d)
	// Output: hello 2 true {bar}
}

func ExampleUnpack5() {
	a, b, c, d, e := Unpack5(T5("hello", 2, true, foo{bar: "bar"}, 4.2))
	fmt.Printf("%v %v %v %v %v", a, b, c, d, e)
	// Output: hello 2 true {bar} 4.2
}

func ExampleUnpack6() {
	a, b, c, d, e, f := Unpack6(T6("hello", 2, true, foo{bar: "bar"}, 4.2, "plop"))
	fmt.Printf("%v %v %v %v %v %v", a, b, c, d, e, f)
	// Output: hello 2 true {bar} 4.2 plop
}

func ExampleUnpack7() {
	a, b, c, d, e, f, g := Unpack7(T7("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false))
	fmt.Printf("%v %v %v %v %v %v %v", a, b, c, d, e, f, g)
	// Output: hello 2 true {bar} 4.2 plop false
}

func ExampleUnpack8() {
	a, b, c, d, e, f, g, h := Unpack8(T8("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42))
	fmt.Printf("%v %v %v %v %v %v %v %v", a, b, c, d, e, f, g, h)
	// Output: hello 2 true {bar} 4.2 plop false 42
}

func ExampleUnpack9() {
	a, b, c, d, e, f, g, h, i := Unpack9(T9("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42, "hello world"))
	fmt.Printf("%v %v %v %v %v %v %v %v %v", a, b, c, d, e, f, g, h, i)
	// Output: hello 2 true {bar} 4.2 plop false 42 hello world
}

func ExampleZip2() {
	result := Zip2([]string{"hello"}, []int{2})
	fmt.Printf("%v", result)
	// Output: [{hello 2}]
}

func ExampleZip3() {
	result := Zip3([]string{"hello"}, []int{2}, []bool{true})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true}]
}

func ExampleZip4() {
	result := Zip4([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar}}]
}

func ExampleZip5() {
	result := Zip5([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}}, []float64{4.2})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar} 4.2}]
}

func ExampleZip6() {
	result := Zip6([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar} 4.2 plop}]
}

func ExampleZip7() {
	result := Zip7([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar} 4.2 plop false}]
}

func ExampleZip8() {
	result := Zip8([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar} 4.2 plop false 42}]
}

func ExampleZip9() {
	result := Zip9([]string{"hello"}, []int{2}, []bool{true}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42}, []string{"hello world"})
	fmt.Printf("%v", result)
	// Output: [{hello 2 true {bar} 4.2 plop false 42 hello world}]
}

func ExampleUnzip2() {
	a, b := Unzip2([]Tuple2[string, int]{T2("hello", 2)})
	fmt.Printf("%v %v", a, b)
	// Output: [hello] [2]
}

func ExampleUnzip3() {
	a, b, c := Unzip3([]Tuple3[string, int, bool]{T3("hello", 2, true)})
	fmt.Printf("%v %v %v", a, b, c)
	// Output: [hello] [2] [true]
}

func ExampleUnzip4() {
	a, b, c, d := Unzip4([]Tuple4[string, int, bool, foo]{T4("hello", 2, true, foo{bar: "bar"})})
	fmt.Printf("%v %v %v %v", a, b, c, d)
	// Output: [hello] [2] [true] [{bar}]
}

func ExampleUnzip5() {
	a, b, c, d, e := Unzip5([]Tuple5[string, int, bool, foo, float64]{T5("hello", 2, true, foo{bar: "bar"}, 4.2)})
	fmt.Printf("%v %v %v %v %v", a, b, c, d, e)
	// Output: [hello] [2] [true] [{bar}] [4.2]
}

func ExampleUnzip6() {
	a, b, c, d, e, f := Unzip6([]Tuple6[string, int, bool, foo, float64, string]{T6("hello", 2, true, foo{bar: "bar"}, 4.2, "plop")})
	fmt.Printf("%v %v %v %v %v %v", a, b, c, d, e, f)
	// Output: [hello] [2] [true] [{bar}] [4.2] [plop]
}

func ExampleUnzip7() {
	a, b, c, d, e, f, g := Unzip7([]Tuple7[string, int, bool, foo, float64, string, bool]{T7("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false)})
	fmt.Printf("%v %v %v %v %v %v %v", a, b, c, d, e, f, g)
	// Output: [hello] [2] [true] [{bar}] [4.2] [plop] [false]
}

func ExampleUnzip8() {
	a, b, c, d, e, f, g, h := Unzip8([]Tuple8[string, int, bool, foo, float64, string, bool, int]{T8("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42)})
	fmt.Printf("%v %v %v %v %v %v %v %v", a, b, c, d, e, f, g, h)
	// Output: [hello] [2] [true] [{bar}] [4.2] [plop] [false] [42]
}

func ExampleUnzip9() {
	a, b, c, d, e, f, g, h, i := Unzip9([]Tuple9[string, int, bool, foo, float64, string, bool, int, string]{T9("hello", 2, true, foo{bar: "bar"}, 4.2, "plop", false, 42, "hello world")})
	fmt.Printf("%v %v %v %v %v %v %v %v %v", a, b, c, d, e, f, g, h, i)
	// Output: [hello] [2] [true] [{bar}] [4.2] [plop] [false] [42] [hello world]
}

func ExampleUnzipByErr2() {
	a, b, err := UnzipByErr2([]string{"hello", "error", "world"}, func(str string) (string, int, error) {
		if str == "error" {
			return "", 0, errors.New("error string not allowed")
		}
		return str, len(str), nil
	})
	fmt.Printf("%v %v %v", len(a), len(b), err)
	// Output: 0 0 error string not allowed
}

func ExampleUnzipByErr3() {
	a, b, c, err := UnzipByErr3([]string{"hello", "error", "world"}, func(str string) (string, int, bool, error) {
		if str == "error" {
			return "", 0, false, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, nil
	})
	fmt.Printf("%v %v %v %v", len(a), len(b), len(c), err)
	// Output: 0 0 0 error string not allowed
}

func ExampleUnzipByErr4() {
	a, b, c, d, err := UnzipByErr4([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, error) {
		if str == "error" {
			return "", 0, false, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v", len(a), len(b), len(c), len(d), err)
	// Output: 0 0 0 0 error string not allowed
}

func ExampleUnzipByErr5() {
	a, b, c, d, e, err := UnzipByErr5([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, float64, error) {
		if str == "error" {
			return "", 0, false, 0, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), float64(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v %v", len(a), len(b), len(c), len(d), len(e), err)
	// Output: 0 0 0 0 0 error string not allowed
}

func ExampleUnzipByErr6() {
	a, b, c, d, e, f, err := UnzipByErr6([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, float64, int8, error) {
		if str == "error" {
			return "", 0, false, 0, 0, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), float64(len(str)), int8(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v %v %v", len(a), len(b), len(c), len(d), len(e), len(f), err)
	// Output: 0 0 0 0 0 0 error string not allowed
}

func ExampleUnzipByErr7() {
	a, b, c, d, e, f, g, err := UnzipByErr7([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, float64, int8, int16, error) {
		if str == "error" {
			return "", 0, false, 0, 0, 0, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), float64(len(str)), int8(len(str)), int16(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v %v %v %v", len(a), len(b), len(c), len(d), len(e), len(f), len(g), err)
	// Output: 0 0 0 0 0 0 0 error string not allowed
}

func ExampleUnzipByErr8() {
	a, b, c, d, e, f, g, h, err := UnzipByErr8([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, float64, int8, int16, int32, error) {
		if str == "error" {
			return "", 0, false, 0, 0, 0, 0, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), float64(len(str)), int8(len(str)), int16(len(str)), int32(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v %v %v %v %v", len(a), len(b), len(c), len(d), len(e), len(f), len(g), len(h), err)
	// Output: 0 0 0 0 0 0 0 0 error string not allowed
}

func ExampleUnzipByErr9() {
	a, b, c, d, e, f, g, h, i, err := UnzipByErr9([]string{"hello", "error", "world"}, func(str string) (string, int, bool, float32, float64, int8, int16, int32, int64, error) {
		if str == "error" {
			return "", 0, false, 0, 0, 0, 0, 0, 0, errors.New("error string not allowed")
		}
		return str, len(str), len(str) > 4, float32(len(str)), float64(len(str)), int8(len(str)), int16(len(str)), int32(len(str)), int64(len(str)), nil
	})
	fmt.Printf("%v %v %v %v %v %v %v %v %v %v", len(a), len(b), len(c), len(d), len(e), len(f), len(g), len(h), len(i), err)
	// Output: 0 0 0 0 0 0 0 0 0 error string not allowed
}

func ExampleCrossJoin2() {
	result := CrossJoin2([]string{"a", "b"}, []int{1, 2, 3, 4})
	for _, r := range result {
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
	result := CrossJoin3([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false})
	for _, r := range result {
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
	result := CrossJoin4([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}})
	for _, r := range result {
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
	result := CrossJoin5([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2})
	for _, r := range result {
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
	result := CrossJoin6([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"})
	for _, r := range result {
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
	result := CrossJoin7([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false})
	for _, r := range result {
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
	result := CrossJoin8([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42})
	for _, r := range result {
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
	result := CrossJoin9([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42}, []string{"hello world"})
	for _, r := range result {
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
	result := CrossJoinBy2([]string{"a", "b"}, []int{1, 2, 3, 4}, func(a string, b int) string {
		return fmt.Sprintf("%v-%v", a, b)
	})
	for _, r := range result {
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
	result := CrossJoinBy3([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, func(a string, b int, c bool) string {
		return fmt.Sprintf("%v-%v-%v", a, b, c)
	})
	for _, r := range result {
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
	result := CrossJoinBy4([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, func(a string, b int, c bool, d foo) string {
		return fmt.Sprintf("%v-%v-%v-%v", a, b, c, d)
	})
	for _, r := range result {
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
	result := CrossJoinBy5([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, func(a string, b int, c bool, d foo, e float64) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v", a, b, c, d, e)
	})
	for _, r := range result {
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
	result := CrossJoinBy6([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, func(a string, b int, c bool, d foo, e float64, f string) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v", a, b, c, d, e, f)
	})
	for _, r := range result {
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
	result := CrossJoinBy7([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, func(a string, b int, c bool, d foo, e float64, f string, g bool) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g)
	})
	for _, r := range result {
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
	result := CrossJoinBy8([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42}, func(a string, b int, c bool, d foo, e float64, f string, g bool, h int) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g, h)
	})
	for _, r := range result {
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
	result := CrossJoinBy9([]string{"a", "b"}, []int{1, 2, 3, 4}, []bool{true, false}, []foo{{bar: "bar"}}, []float64{4.2}, []string{"plop"}, []bool{false}, []int{42}, []string{"hello world"}, func(a string, b int, c bool, d foo, e float64, f string, g bool, h int, i string) string {
		return fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v-%v-%v", a, b, c, d, e, f, g, h, i)
	})
	for _, r := range result {
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

func ExampleCrossJoinByErr2() {
	result, err := CrossJoinByErr2([]string{"a", "b"}, []int{1, 2}, func(a string, b int) (string, error) {
		if a == "b" {
			return "", errors.New("b not allowed")
		}
		return fmt.Sprintf("%v-%v", a, b), nil
	})
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	for _, r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// error: b not allowed
}

func ExampleZipByErr2() {
	result, err := ZipByErr2([]string{"a", "b", "c"}, []int{1, 2, 3}, func(a string, b int) (string, error) {
		if a == "b" {
			return "", errors.New("b is not allowed")
		}
		return fmt.Sprintf("%v-%v", a, b), nil
	})
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	for _, r := range result {
		fmt.Printf("%v\n", r)
	}
	// Output:
	// error: b is not allowed
}

func ExampleIntersect() {
	result := Intersect([]int{0, 3, 5, 7}, []int{3, 5}, []int{0, 1, 2, 0, 3, 0})
	fmt.Printf("%v", result)
	// Output:
	// [3]
}

func ExampleIntersectBy() {
	result := IntersectBy(strconv.Itoa, []int{0, 6, 0, 3}, []int{0, 1, 2, 3, 4, 5}, []int{0, 6})
	fmt.Printf("%v", result)
	// Output:
	// [0]
}
