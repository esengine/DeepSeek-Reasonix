// Copyright (c) 2017-2023 Uber Technologies, Inc.
//
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

package stack

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _allDone chan struct{}

func waitForDone() {
	<-_allDone
}

func TestAll(t *testing.T) {
	// We use a global channel so that the function below does not
	// receive any arguments, so we can test that parseFirstFunc works
	// regardless of arguments on the stack.
	_allDone = make(chan struct{})
	defer close(_allDone)

	for i := 0; i < 5; i++ {
		go waitForDone()
	}

	cur := Current()
	got := All()

	// Retry until the background stacks are not runnable/running.
	for {
		if !isBackgroundRunning(cur, got) {
			break
		}
		runtime.Gosched()
		got = All()
	}

	// We have exactly 7 gorotuines:
	// "main" goroutine
	// test goroutine
	// 5 goroutines started above.
	require.Len(t, got, 7)
	sort.Sort(byGoroutineID(got))

	assert.Contains(t, got[0].Full(), "testing.(*T).Run")
	assert.Contains(t, got[0].allFunctions, "testing.(*T).Run")

	assert.Contains(t, got[1].Full(), "TestAll")
	assert.Contains(t, got[1].allFunctions, "go.uber.org/goleak/internal/stack.TestAll")

	for i := 0; i < 5; i++ {
		assert.Contains(t, got[2+i].Full(), "stack.waitForDone")
	}
}

func TestCurrent(t *testing.T) {
	const pkgPrefix = "go.uber.org/goleak/internal/stack"

	got := Current()
	assert.NotZero(t, got.ID(), "Should get non-zero goroutine id")
	assert.Equal(t, "running", got.State())
	assert.Equal(t, "go.uber.org/goleak/internal/stack.getStackBuffer", got.FirstFunction())

	wantFrames := []string{
		"getStackBuffer",
		"getStacks",
		"Current",
		"Current",
		"TestCurrent",
	}
	all := got.Full()
	for _, frame := range wantFrames {
		name := pkgPrefix + "." + frame
		assert.Contains(t, all, name)
		assert.True(t, got.HasFunction(name), "missing in stack: %v\n%s", name, all)
	}
	assert.Contains(t, got.String(), "in state")
	assert.Contains(t, got.String(), "on top of the stack")

	assert.Contains(t, all, "stack/stacks_test.go",
		"file name missing in stack:\n%s", all)

	// Ensure that we are not returning the buffer without slicing it
	// from getStackBuffer.
	if len(got.Full()) > 1024 {
		t.Fatalf("Returned stack is too large")
	}
}

func TestCurrentCreatedBy(t *testing.T) {
	var stack Stack
	done := make(chan struct{})
	go func() {
		defer close(done)
		stack = Current()
	}()
	<-done

	// The test function created the goroutine
	// so it won't be part of the stack.
	assert.False(t, stack.HasFunction("go.uber.org/goleak/internal/stack.TestCurrentCreatedBy"),
		"TestCurrentCreatedBy should not be in stack:\n%s", stack.Full())

	// However, the nested function should be.
	assert.True(t,
		stack.HasFunction("go.uber.org/goleak/internal/stack.TestCurrentCreatedBy.func1"),
		"TestCurrentCreatedBy.func1 is not in stack:\n%s", stack.Full())
}

func TestAllLargeStack(t *testing.T) {
	const (
		stackDepth    = 100
		numGoroutines = 100
	)

	var started sync.WaitGroup

	done := make(chan struct{})
	for i := 0; i < numGoroutines; i++ {
		var f func(int)
		f = func(count int) {
			if count == 0 {
				started.Done()
				<-done
				return
			}
			f(count - 1)
		}
		started.Add(1)
		go f(stackDepth)
	}

	started.Wait()
	buf := getStackBuffer(true /* all */)
	if len(buf) <= _defaultBufferSize {
		t.Fatalf("Expected larger stack buffer")
	}

	// Start enough goroutines so we exceed the default buffer size.
	close(done)
}

func TestParseFuncName(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    string
		creator bool
	}{
		{
			name: "function",
			give: "example.com/foo/bar.baz()",
			want: "example.com/foo/bar.baz",
		},
		{
			name: "method",
			give: "example.com/foo/bar.(*baz).qux()",
			want: "example.com/foo/bar.(*baz).qux",
		},
		{
			name:    "created by", // Go 1.20
			give:    "created by example.com/foo/bar.baz",
			want:    "example.com/foo/bar.baz",
			creator: true,
		},
		{
			name:    "created by/in goroutine", // Go 1.21
			give:    "created by example.com/foo/bar.baz in goroutine 123",
			want:    "example.com/foo/bar.baz",
			creator: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, creator, err := parseFuncName(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.creator, creator)
		})
	}
}

func TestParseStack(t *testing.T) {
	tests := []struct {
		name string
		give string

		id        int
		state     string
		firstFunc string
		funcs     []string
	}{
		{
			name: "running",
			give: joinLines(
				"goroutine 1 [running]:",
				"example.com/foo/bar.baz()",
				"	example.com/foo/bar.go:123",
			),
			id:        1,
			state:     "running",
			firstFunc: "example.com/foo/bar.baz",
			funcs:     []string{"example.com/foo/bar.baz"},
		},
		{
			name: "without position",
			give: joinLines(
				"goroutine 1 [running]:",
				"example.com/foo/bar.baz()",
				// Oops, no "file:line" entry for this function.
				"example.com/foo/bar.qux()",
				"	example.com/foo/bar.go:456",
			),
			id:        1,
			state:     "running",
			firstFunc: "example.com/foo/bar.baz",
			funcs: []string{
				"example.com/foo/bar.baz",
				"example.com/foo/bar.qux",
			},
		},
		{
			name: "created by",
			give: joinLines(
				"goroutine 1 [running]:",
				"example.com/foo/bar.baz()",
				"	example.com/foo/bar.go:123",
				"created by example.com/foo/bar.qux",
				"	example.com/foo/bar.go:456",
			),
			id:        1,
			state:     "running",
			firstFunc: "example.com/foo/bar.baz",
			funcs: []string{
				"example.com/foo/bar.baz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stacks, err := newStackParser(strings.NewReader(tt.give)).Parse()
			require.NoError(t, err)
			require.Len(t, stacks, 1)

			stack := stacks[0]
			assert.Equal(t, tt.id, stack.ID())
			assert.Equal(t, tt.state, stack.State())
			assert.Equal(t, tt.firstFunc, stack.FirstFunction())
			for _, fn := range tt.funcs {
				assert.True(t, stack.HasFunction(fn),
					"missing in stack: %v\n%s", fn, stack.Full())
			}
		})
	}
}

func TestParseStackErrors(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantErr string
	}{
		{
			name:    "bad goroutine ID",
			give:    "goroutine no-number [running]:",
			wantErr: `bad goroutine ID "no-number"`,
		},
		{
			name:    "not enough parts",
			give:    "goroutine [running]:",
			wantErr: `unexpected format`,
		},
		{
			name: "bad function name",
			give: joinLines(
				"goroutine 1 [running]:",
				"example.com/foo/bar.baz", // no arguments
				"	example.com/foo/bar.go:123",
			),
			wantErr: `no function found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newStackParser(strings.NewReader(tt.give)).Parse()
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestParseStackFixtures(t *testing.T) {
	type goroutine struct {
		// ID must match the goroutine ID in the fixture.
		// We use this to ensure that we are matching the right goroutine.
		ID int

		State         string
		FirstFunction string

		HasFunctions    []string // non-exhaustive, in any order
		NotHasFunctions []string
	}

	tests := []struct {
		name   string      // file name inside testdata
		stacks []goroutine // in any order
	}{
		{
			name: "http.txt",
			stacks: []goroutine{
				{
					ID:            1,
					State:         "running",
					FirstFunction: "main.getStackBuffer",
					HasFunctions: []string{
						"main.getStackBuffer",
						"main.main",
					},
				},
				{
					ID:            4,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.Serve",
					},
					NotHasFunctions: []string{"main.start"},
				},
				{
					ID:            20,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).readLoop",
				},
				{
					ID:            21,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).writeLoop",
				},
				{
					ID:            8,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.(*conn).serve",
					},
					NotHasFunctions: []string{"net/http.(*Server).Serve"},
				},
			},
		},
		{
			name: "http.go1.20.txt",
			stacks: []goroutine{
				{
					ID:            1,
					State:         "running",
					FirstFunction: "main.getStackBuffer",
					HasFunctions: []string{
						"main.getStackBuffer",
						"main.main",
					},
				},
				{
					ID:            20,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.(*Server).Serve",
					},
					NotHasFunctions: []string{"main.start"},
				},
				{
					ID:            24,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).readLoop",
				},
				{
					ID:            25,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).writeLoop",
				},
				{
					ID:            4,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.(*conn).serve",
					},
					NotHasFunctions: []string{"net/http.(*Server).Serve"},
				},
			},
		},
		{
			name: "http.tracebackancestors.txt",
			stacks: []goroutine{
				{
					ID:            1,
					State:         "running",
					FirstFunction: "main.getStackBuffer",
					HasFunctions: []string{
						"main.getStackBuffer",
						"main.main",
					},
				},
				{
					ID:            20,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.Serve",
					},
					NotHasFunctions: []string{
						"main.start", // created by
						"main.main",  // tracebackancestors
					},
				},
				{
					ID:            24,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).readLoop",
					NotHasFunctions: []string{
						"net/http.(*Transport).dialConn", // created by
						// tracebackancestors:
						"net/http.(*Transport).dialConnFor",
						"net/http.(*Transport).queueForDial",
						"net/http.(*Client).Get",
						"main.start",
						"main.main",
					},
				},
				{
					ID:            4,
					State:         "IO wait",
					FirstFunction: "internal/poll.runtime_pollWait",
					HasFunctions: []string{
						"internal/poll.runtime_pollWait",
						"net/http.(*conn).serve",
					},
					NotHasFunctions: []string{
						"net/http.(*Server).Serve", // created by
						// tracebackancestors:
						"net/http.Serve",
						"main.start",
						"main.main",
					},
				},
				{
					ID:            25,
					State:         "select",
					FirstFunction: "net/http.(*persistConn).writeLoop",
					NotHasFunctions: []string{
						"net/http.(*Transport).dialConn", // created by
						// tracebackancestors:
						"net/http.(*Transport).dialConnFor",
						"net/http.(*Transport).queueForDial",
						"net/http.(*Client).Get",
						"main.start",
						"main.main",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, err := os.Open(filepath.Join("testdata", tt.name))
			require.NoError(t, err)
			defer func() {
				assert.NoError(t, fixture.Close())
			}()

			stacks, err := newStackParser(fixture).Parse()
			require.NoError(t, err)

			stacksByID := make(map[int]Stack, len(stacks))
			for _, s := range stacks {
				stacksByID[s.ID()] = s
			}

			for _, wantStack := range tt.stacks {
				gotStack, ok := stacksByID[wantStack.ID]
				if !assert.True(t, ok, "missing stack %v", wantStack.ID) {
					continue
				}
				delete(stacksByID, wantStack.ID)

				assert.Equal(t, wantStack.State, gotStack.State())
				assert.Equal(t, wantStack.FirstFunction, gotStack.FirstFunction())

				for _, fn := range wantStack.HasFunctions {
					assert.True(t, gotStack.HasFunction(fn), "missing in stack: %v\n%s", fn, gotStack.Full())
				}

				for _, fn := range wantStack.NotHasFunctions {
					assert.False(t, gotStack.HasFunction(fn), "unexpected in stack: %v\n%s", fn, gotStack.Full())
				}
			}

			for _, s := range stacksByID {
				t.Errorf("unexpected stack:\n%s", s.Full())
			}
		})
	}
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

type byGoroutineID []Stack

func (ss byGoroutineID) Len() int           { return len(ss) }
func (ss byGoroutineID) Less(i, j int) bool { return ss[i].ID() < ss[j].ID() }
func (ss byGoroutineID) Swap(i, j int)      { ss[i], ss[j] = ss[j], ss[i] }

// Note: This is the same logic as in ../../utils_test.go
// Copy+pasted to avoid dependency loops and exporting this test-helper.
func isBackgroundRunning(cur Stack, stacks []Stack) bool {
	for _, s := range stacks {
		if cur.ID() == s.ID() {
			continue
		}

		if strings.Contains(s.State(), "run") {
			return true
		}
	}

	return false
}
