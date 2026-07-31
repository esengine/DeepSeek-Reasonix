//go:build !race

package lo

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

func ExampleNewDebounce() {
	i := int32(0)
	calls := []int32{}
	mu := sync.Mutex{}

	debounce, cancel := NewDebounce(time.Millisecond, func() {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, atomic.LoadInt32(&i))
	})

	debounce()
	atomic.AddInt32(&i, 1)

	time.Sleep(5 * time.Millisecond)

	debounce()
	atomic.AddInt32(&i, 1)
	debounce()
	atomic.AddInt32(&i, 1)
	debounce()
	atomic.AddInt32(&i, 1)

	time.Sleep(5 * time.Millisecond)

	cancel()

	mu.Lock()
	fmt.Printf("%v", calls)
	mu.Unlock()
	// Output: [1 4]
}

func ExampleNewDebounceBy() {
	calls := map[string][]int{}
	mu := sync.Mutex{}

	debounce, cancel := NewDebounceBy(time.Millisecond, func(userID string, count int) {
		mu.Lock()
		defer mu.Unlock()

		if _, ok := calls[userID]; !ok {
			calls[userID] = []int{}
		}

		calls[userID] = append(calls[userID], count)
	})

	debounce("samuel")
	debounce("john")

	time.Sleep(5 * time.Millisecond)

	debounce("john")
	debounce("john")
	debounce("samuel")
	debounce("john")

	time.Sleep(5 * time.Millisecond)

	cancel("samuel")
	cancel("john")

	mu.Lock()
	fmt.Printf("samuel: %v\n", calls["samuel"])
	fmt.Printf("john: %v\n", calls["john"])
	mu.Unlock()
	// Output:
	// samuel: [1 1]
	// john: [1 3]
}

func ExampleAttempt() {
	count1, err1 := Attempt(2, func(i int) error {
		if i == 0 {
			return errors.New("error")
		}

		return nil
	})

	count2, err2 := Attempt(2, func(i int) error {
		if i < 10 {
			return errors.New("error")
		}

		return nil
	})

	fmt.Printf("%v %v\n", count1, err1)
	fmt.Printf("%v %v\n", count2, err2)
	// Output:
	// 2 <nil>
	// 2 error
}

func ExampleAttemptWithDelay() {
	count1, time1, err1 := AttemptWithDelay(2, time.Millisecond, func(i int, _ time.Duration) error {
		if i == 0 {
			return errors.New("error")
		}

		return nil
	})

	count2, time2, err2 := AttemptWithDelay(2, time.Millisecond, func(i int, _ time.Duration) error {
		if i < 10 {
			return errors.New("error")
		}

		return nil
	})

	fmt.Printf("%v %v %v\n", count1, time1.Truncate(time.Millisecond), err1)
	fmt.Printf("%v %v %v\n", count2, time2.Truncate(time.Millisecond), err2)
	// Output:
	// 2 1ms <nil>
	// 2 1ms error
}

func ExampleTransaction() {
	transaction := NewTransaction[int]().
		Then(
			func(state int) (int, error) {
				fmt.Println("step 1")
				return state + 10, nil
			},
			func(state int) int {
				fmt.Println("rollback 1")
				return state - 10
			},
		).
		Then(
			func(state int) (int, error) {
				fmt.Println("step 2")
				return state + 15, nil
			},
			func(state int) int {
				fmt.Println("rollback 2")
				return state - 15
			},
		).
		Then(
			func(state int) (int, error) {
				fmt.Println("step 3")

				if true {
					return state, errors.New("error")
				}

				return state + 42, nil
			},
			func(state int) int {
				fmt.Println("rollback 3")
				return state - 42
			},
		)

	_, _ = transaction.Process(-5)

	// Output:
	// step 1
	// step 2
	// step 3
	// rollback 2
	// rollback 1
}

func ExampleTransaction_ok() {
	transaction := NewTransaction[int]().
		Then(
			func(state int) (int, error) {
				return state + 10, nil
			},
			func(state int) int {
				return state - 10
			},
		).
		Then(
			func(state int) (int, error) {
				return state + 15, nil
			},
			func(state int) int {
				return state - 15
			},
		).
		Then(
			func(state int) (int, error) {
				return state + 42, nil
			},
			func(state int) int {
				return state - 42
			},
		)

	state, err := transaction.Process(-5)

	fmt.Println(state)
	fmt.Println(err)
	// Output:
	// 62
	// <nil>
}

func ExampleTransaction_error() {
	transaction := NewTransaction[int]().
		Then(
			func(state int) (int, error) {
				return state + 10, nil
			},
			func(state int) int {
				return state - 10
			},
		).
		Then(
			func(state int) (int, error) {
				return state, errors.New("error")
			},
			func(state int) int {
				return state - 15
			},
		).
		Then(
			func(state int) (int, error) {
				return state + 42, nil
			},
			func(state int) int {
				return state - 42
			},
		)

	state, err := transaction.Process(-5)

	fmt.Println(state)
	fmt.Println(err)
	// Output:
	// -5
	// error
}

func ExampleNewThrottle() {
	throttle, reset := NewThrottle(100*time.Millisecond, func() {
		fmt.Println("Called once in every 100ms")
	})

	for j := 0; j < 10; j++ {
		throttle()
		time.Sleep(30 * time.Millisecond)
	}

	reset()

	// Output:
	// Called once in every 100ms
	// Called once in every 100ms
	// Called once in every 100ms
}

func ExampleNewThrottleWithCount() {
	throttle, reset := NewThrottleWithCount(100*time.Millisecond, 2, func() {
		fmt.Println("Called once in every 100ms")
	})

	for j := 0; j < 10; j++ {
		throttle()
		time.Sleep(30 * time.Millisecond)
	}

	reset()

	// Output:
	// Called once in every 100ms
	// Called once in every 100ms
	// Called once in every 100ms
	// Called once in every 100ms
	// Called once in every 100ms
	// Called once in every 100ms
}

func ExampleNewThrottleBy() {
	throttle, reset := NewThrottleBy(100*time.Millisecond, func(key string) {
		fmt.Println(key, "Called once in every 100ms")
	})

	for j := 0; j < 10; j++ {
		throttle("foo")
		throttle("bar")
		time.Sleep(30 * time.Millisecond)
	}

	reset()

	// Output:
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
}

func ExampleNewThrottleByWithCount() {
	throttle, reset := NewThrottleByWithCount(100*time.Millisecond, 2, func(key string) {
		fmt.Println(key, "Called once in every 100ms")
	})

	for j := 0; j < 10; j++ {
		throttle("foo")
		throttle("bar")
		time.Sleep(30 * time.Millisecond)
	}

	reset()

	// Output:
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
	// foo Called once in every 100ms
	// bar Called once in every 100ms
}
