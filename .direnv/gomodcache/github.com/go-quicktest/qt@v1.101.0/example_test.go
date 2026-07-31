package qt_test

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func ExampleComment() {
	runExampleTest(func(t testing.TB) {
		a := 42
		qt.Assert(t, qt.Equals(a, 42), qt.Commentf("no answer to life, the universe, and everything"))
	})
	// Output: PASS
}

func ExampleEquals() {
	runExampleTest(func(t testing.TB) {
		answer := int64(42)
		qt.Assert(t, qt.Equals(answer, 42))
	})
	// Output: PASS
}

func ExampleDeepEquals() {
	runExampleTest(func(t testing.TB) {
		list := []int{42, 47}
		qt.Assert(t, qt.DeepEquals(list, []int{42, 47}))
	})
	// Output: PASS
}

func ExampleCmpEquals() {
	runExampleTest(func(t testing.TB) {
		list := []int{42, 47}
		qt.Assert(t, qt.CmpEquals(list, []int{47, 42}, cmpopts.SortSlices(func(i, j int) bool {
			return i < j
		})))
	})
	// Output: PASS
}

type myStruct struct {
	a int
}

func customDeepEquals[T any](got, want T) qt.Checker {
	return qt.CmpEquals(got, want, cmp.AllowUnexported(myStruct{}))
}

func ExampleCmpEquals_customfunc() {
	runExampleTest(func(t testing.TB) {
		got := &myStruct{
			a: 1234,
		}
		qt.Assert(t, customDeepEquals(got, &myStruct{
			a: 1234,
		}))
	})
	// Output: PASS
}

func ExampleContentEquals() {
	runExampleTest(func(t testing.TB) {
		got := []int{1, 23, 4, 5}
		qt.Assert(t, qt.ContentEquals(got, []int{1, 4, 5, 23}))
	})
	// Output: PASS
}

func ExampleMatches() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.Matches("these are the voyages", "these are .*"))
		qt.Assert(t, qt.Matches(net.ParseIP("1.2.3.4").String(), "1.*"))
	})
	// Output: PASS
}

func ExampleErrorMatches() {
	runExampleTest(func(t testing.TB) {
		err := errors.New("bad wolf at the door")
		qt.Assert(t, qt.ErrorMatches(err, "bad wolf .*"))
	})
	// Output: PASS
}

func ExamplePanicMatches() {
	runExampleTest(func(t testing.TB) {
		divide := func(a, b int) int {
			return a / b
		}
		qt.Assert(t, qt.PanicMatches(func() {
			divide(5, 0)
		}, "runtime error: .*"))
	})
	// Output: PASS
}

func ExampleIsNil() {
	runExampleTest(func(t testing.TB) {
		got := (*int)(nil)
		qt.Assert(t, qt.IsNil(got))
	})
	// Output: PASS
}

func ExampleIsNotNil() {
	runExampleTest(func(t testing.TB) {
		got := new(int)

		qt.Assert(t, qt.IsNotNil(got))

		// Note that unlike reflection-based APIs, a nil
		// value inside an interface still counts as non-nil,
		// just as if we were comparing the actual interface
		// value against nil.
		nilValueInInterface := any((*int)(nil))
		qt.Assert(t, qt.IsNotNil(nilValueInInterface))
	})
	// Output: PASS
}

func ExampleHasLen() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.HasLen([]int{42, 47}, 2))

		myMap := map[string]int{
			"a": 13,
			"b": 4,
			"c": 10,
		}
		qt.Assert(t, qt.HasLen(myMap, 3))
	})
	// Output: PASS
}

func ExampleImplements() {
	runExampleTest(func(t testing.TB) {
		var myReader struct {
			io.ReadCloser
		}
		qt.Assert(t, qt.Implements[io.ReadCloser](myReader))
	})
	// Output: PASS
}

func ExampleSatisfies() {
	runExampleTest(func(t testing.TB) {
		// Check that an error from os.Open satisfies os.IsNotExist.
		_, err := os.Open("/non-existent-file")
		qt.Assert(t, qt.Satisfies(err, os.IsNotExist))

		// Check that a floating point number is a not-a-number.
		f := math.NaN()
		qt.Assert(t, qt.Satisfies(f, math.IsNaN))

	})
	// Output: PASS
}

func ExampleIsTrue() {
	runExampleTest(func(t testing.TB) {
		isValid := func() bool {
			return true
		}
		qt.Assert(t, qt.IsTrue(1 == 1))
		qt.Assert(t, qt.IsTrue(isValid()))
	})
	// Output: PASS

}

func ExampleIsFalse() {
	runExampleTest(func(t testing.TB) {
		isValid := func() bool {
			return false
		}
		qt.Assert(t, qt.IsFalse(1 == 0))
		qt.Assert(t, qt.IsFalse(isValid()))
	})
	// Output: PASS

}

func ExampleNot() {
	runExampleTest(func(t testing.TB) {

		got := []int{1, 2}
		qt.Assert(t, qt.Not(qt.IsNil(got)))

		answer := 13
		qt.Assert(t, qt.Not(qt.Equals(answer, 42)))
	})
	// Output: PASS
}

func ExampleStringContains() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.StringContains("hello world", "hello"))
	})
	// Output: PASS
}

func ExampleSliceContains() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.SliceContains([]int{3, 5, 7, 99}, 99))
		qt.Assert(t, qt.SliceContains([]string{"a", "cd", "e"}, "cd"))
	})
	// Output: PASS
}

func ExampleMapContains() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.MapContains(map[string]int{
			"hello": 1234,
		}, 1234))
	})
	// Output: PASS
}

func ExampleSliceAny() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.SliceAny([]int{3, 5, 7, 99}, qt.F2(qt.Equals[int], 7)))
		qt.Assert(t, qt.SliceAny([][]string{{"a", "b"}, {"c", "d"}}, qt.F2(qt.DeepEquals[[]string], []string{"c", "d"})))
	})
	// Output: PASS
}

func ExampleMapAny() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.MapAny(map[string]int{"x": 2, "y": 3}, qt.F2(qt.Equals[int], 3)))
	})
	// Output: PASS
}

func ExampleSliceAll() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.SliceAll([]int{3, 5, 8}, func(e int) qt.Checker {
			return qt.Not(qt.Equals(e, 0))
		}))
		qt.Assert(t, qt.SliceAll([][]string{{"a", "b"}, {"a", "b"}}, qt.F2(qt.DeepEquals[[]string], []string{"a", "b"})))
	})
	// Output: PASS
}

func ExampleMapAll() {
	runExampleTest(func(t testing.TB) {
		qt.Assert(t, qt.MapAll(map[string]int{
			"x": 2,
			"y": 2,
		}, qt.F2(qt.Equals[int], 2)))
	})
	// Output: PASS
}

func ExampleJSONEquals() {
	runExampleTest(func(t testing.TB) {
		data := `[1, 2, 3]`
		qt.Assert(t, qt.JSONEquals(data, []uint{1, 2, 3}))
	})
	// Output: PASS
}

func ExampleErrorAs() {
	runExampleTest(func(t testing.TB) {
		_, err := os.Open("/non-existent-file")

		// Checking for a specific error type.
		qt.Assert(t, qt.ErrorAs(err, new(*os.PathError)))
		qt.Assert(t, qt.ErrorAs[*os.PathError](err, nil))

		// Checking fields on a specific error type.
		var pathError *os.PathError
		if qt.Check(t, qt.ErrorAs(err, &pathError)) {
			qt.Assert(t, qt.Equals(pathError.Path, "/non-existent-file"))
		}
	})
	// Output: PASS
}

func ExampleErrorIs() {
	runExampleTest(func(t testing.TB) {
		_, err := os.Open("/non-existent-file")

		qt.Assert(t, qt.ErrorIs(err, os.ErrNotExist))
	})
	// Output: PASS
}

func runExampleTest(f func(t testing.TB)) {
	defer func() {
		if err := recover(); err != nil && err != exampleTestFatal {
			panic(err)
		}
	}()
	var t exampleTestingT
	f(&t)
	if t.failed {
		fmt.Println("FAIL")
	} else {
		fmt.Println("PASS")
	}
}

type exampleTestingT struct {
	testing.TB
	failed bool
}

var exampleTestFatal = errors.New("example test fatal error")

func (t *exampleTestingT) Helper() {}

func (t *exampleTestingT) Error(args ...any) {
	fmt.Printf("ERROR: %s\n", fmt.Sprint(args...))
	t.failed = true
}

func (t *exampleTestingT) Fatal(args ...any) {
	fmt.Printf("FATAL: %s\n", fmt.Sprint(args...))
	t.failed = true
	panic(exampleTestFatal)
}
