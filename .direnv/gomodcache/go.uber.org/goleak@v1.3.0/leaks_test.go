// Copyright (c) 2017 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package goleak

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensure that testingT is a subset of testing.TB.
var _ = TestingT(testing.TB(nil))

// testOptions passes a shorter max sleep time, used so tests don't wait
// ~1 second in cases where we expect Find to error out.
func testOptions() Option {
	return maxSleep(time.Millisecond)
}

func TestFind(t *testing.T) {
	t.Run("Should find no leaks by default", func(t *testing.T) {
		require.NoError(t, Find())
	})

	t.Run("Find leaks with leaked goroutine", func(t *testing.T) {
		bg := startBlockedG()
		err := Find(testOptions())
		require.Error(t, err, "Should find leaks with leaked goroutine")
		assert.ErrorContains(t, err, "blockedG")
		assert.ErrorContains(t, err, "created by go.uber.org/goleak.startBlockedG")

		// Once we unblock the goroutine, we shouldn't have leaks.
		bg.unblock()
		require.NoError(t, Find(), "Should find no leaks by default")
	})

	t.Run("Find can't take in Cleanup option", func(t *testing.T) {
		err := Find(Cleanup(func(int) { assert.Fail(t, "this should not be called") }))
		require.Error(t, err, "Should exit with invalid option")
	})
}

func TestFindRetry(t *testing.T) {
	// for i := 0; i < 10; i++ {
	bg := startBlockedG()
	require.Error(t, Find(testOptions()), "Should find leaks with leaked goroutine")

	go func() {
		time.Sleep(time.Millisecond)
		bg.unblock()
	}()
	require.NoError(t, Find(), "Find should retry while background goroutine ends")
}

type fakeT struct {
	errors []string
}

func (ft *fakeT) Error(args ...interface{}) {
	ft.errors = append(ft.errors, fmt.Sprint(args...))
}

func TestVerifyNone(t *testing.T) {
	t.Run("VerifyNone finds leaks", func(t *testing.T) {
		ft := &fakeT{}
		VerifyNone(ft)
		require.Empty(t, ft.errors, "Expect no errors from VerifyNone")

		bg := startBlockedG()
		VerifyNone(ft, testOptions())
		require.NotEmpty(t, ft.errors, "Expect errors from VerifyNone on leaked goroutine")
		bg.unblock()
	})

	t.Run("cleanup registered callback should be called", func(t *testing.T) {
		ft := &fakeT{}
		cleanupCalled := false
		VerifyNone(ft, Cleanup(func(c int) {
			assert.Equal(t, 0, c)
			cleanupCalled = true
		}))
		require.True(t, cleanupCalled, "expect cleanup registered callback to be called")
	})
}

func TestIgnoreCurrent(t *testing.T) {
	t.Run("Should ignore current", func(t *testing.T) {
		defer VerifyNone(t)

		done := make(chan struct{})
		go func() {
			<-done
		}()

		// We expect the above goroutine to be ignored.
		VerifyNone(t, IgnoreCurrent())
		close(done)
	})

	t.Run("Should detect new leaks", func(t *testing.T) {
		defer VerifyNone(t)

		// There are no leaks currently.
		VerifyNone(t)

		done1 := make(chan struct{})
		done2 := make(chan struct{})

		go func() {
			<-done1
		}()

		err := Find()
		require.Error(t, err, "Expected to find background goroutine as leak")

		opt := IgnoreCurrent()
		VerifyNone(t, opt)

		// A second goroutine started after IgnoreCurrent is a leak
		go func() {
			<-done2
		}()

		err = Find(opt)
		require.Error(t, err, "Expect second goroutine to be flagged as a leak")

		close(done1)
		close(done2)
	})

	t.Run("Should not ignore false positive", func(t *testing.T) {
		defer VerifyNone(t)

		const goroutinesCount = 5
		var wg sync.WaitGroup
		done := make(chan struct{})

		// Spawn few goroutines before checking leaks
		for i := 0; i < goroutinesCount; i++ {
			wg.Add(1)
			go func() {
				<-done
				wg.Done()
			}()
		}

		// Store all goroutines
		option := IgnoreCurrent()

		// Free goroutines
		close(done)
		wg.Wait()

		// We expect the below goroutines to be founded.
		for i := 0; i < goroutinesCount; i++ {
			ch := make(chan struct{})

			go func() {
				<-ch
			}()

			require.Error(t, Find(option), "Expect spawned goroutine to be flagged as a leak")

			// Free spawned goroutine
			close(ch)

			// Make sure that there are no leaks
			VerifyNone(t)
		}
	})
}

func TestVerifyParallel(t *testing.T) {
	t.Run("parallel", func(t *testing.T) {
		t.Parallel()
	})

	t.Run("serial", func(t *testing.T) {
		VerifyNone(t)
	})
}
