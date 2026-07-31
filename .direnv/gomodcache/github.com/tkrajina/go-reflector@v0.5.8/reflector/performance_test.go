package reflector

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

func stopwatch(n int, title string) func() {
	start := time.Now()
	return func() {
		fmt.Println(title)
		fmt.Printf("%12s= %d\n", "n", n)
		const dateFormat = "2006-01-02 15:04:05.123"
		fmt.Printf("%12s: %s\n", "started", start.Format(dateFormat))
		fmt.Printf("%12s: %fs\n", "duration", time.Since(start).Seconds())
	}
}

func performanceN(envN string) int {
	n, _ := strconv.ParseInt(envN, 10, 64)
	const defaultN = 1000
	if n < 1 {
		n = defaultN
	}
	return int(n)
}

// Utility to test performance of some typical operations, run with:
// N=1000000 go test -v ./... -run=TestPerformance
// Iy you change anything here, change TestPerformance_plain too!
func TestPerformance_reflection(t *testing.T) {
	t.Parallel()
	n := performanceN(os.Getenv("N"))
	defer stopwatch(n, "WITH REFLECTION")()
	for i := 0; i < n; i++ {
		p := &Person{}
		obj := New(p)

		err := obj.Field("Number").Set(i)
		if err != nil {
			t.Fatal("Should not error")
		}
		if p.Number != i {
			t.Fatalf("Should be %d", i)
		}
		number, err := obj.Field("Number").Get()
		if err != nil {
			t.Fatal("Number is valid")
		}
		if number.(int) != i {
			t.Fatalf("Should be %d", i)
		}
		res, err := obj.Method("Add").Call(1, 2, 3)
		if err != nil {
			t.Fatal("shouldn't be an error")
		}
		if res.IsError() {
			t.Fatal("method shouldn't return an error")
		}
		if len(res.Result) != 1 && res.Result[0].(int) != 6 {
			t.Fatal("result should be 6")
		}
	}
}

func TestPerformance_plain(t *testing.T) {
	t.Parallel()
	n := performanceN(os.Getenv("N"))
	defer stopwatch(n, "WITHOUT REFLECTION")()
	for i := 0; i < n; i++ {
		p := &Person{}

		p.Number = i
		number := p.Number
		if number != i {
			t.Fatalf("Should be %d", i)
		}
		res := p.Add(1, 2, 3)
		if res != 6 {
			t.Fatal("result should be 6")
		}
	}
}
