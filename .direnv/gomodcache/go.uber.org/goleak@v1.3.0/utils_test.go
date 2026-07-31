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
	"runtime"
	"strings"
	"testing"

	"go.uber.org/goleak/internal/stack"
)

type blockedG struct {
	started chan struct{}
	wait    chan struct{}
}

func startBlockedG() *blockedG {
	bg := &blockedG{
		started: make(chan struct{}),
		wait:    make(chan struct{}),
	}
	go bg.run()
	<-bg.started
	return bg
}

func (bg *blockedG) run() {
	close(bg.started)
	bg.block()
}

func (bg *blockedG) block() {
	<-bg.wait
}

func (bg *blockedG) unblock() {
	close(bg.wait)
}

func getStableAll(t *testing.T, cur stack.Stack) []stack.Stack {
	all := stack.All()

	// There may be running goroutines that were just scheduled or finishing up
	// from previous tests, so reduce flakiness by waiting till no other goroutines
	// are runnable or running except the current goroutine.
	for retry := 0; true; retry++ {
		if !isBackgroundRunning(cur, all) {
			break
		}

		if retry >= 100 {
			t.Fatalf("background goroutines are possibly running, %v", all)
		}

		runtime.Gosched()
		all = stack.All()
	}

	return all
}

// Note: This is the same logic as in internal/stacks/stacks_test.go
func isBackgroundRunning(cur stack.Stack, stacks []stack.Stack) bool {
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
