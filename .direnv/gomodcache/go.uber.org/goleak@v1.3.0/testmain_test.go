// Copyright (c) 2017 Uber Technologies, Inc.
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

package goleak

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func init() {
	clearOSStubs()
}

func clearOSStubs() {
	// We don't want to use the real os.Exit or os.Stderr so nil them out.
	// Tests MUST set them explicitly if they rely on them.
	_osExit = nil
	_osStderr = nil
}

type dummyTestMain int

func (d dummyTestMain) Run() int {
	return int(d)
}

func osStubs() (chan int, chan string) {
	exitCode := make(chan int, 1)
	stderr := make(chan string, 1)

	buf := &bytes.Buffer{}
	_osStderr = buf
	_osExit = func(code int) {
		exitCode <- code
		stderr <- buf.String()
		buf.Reset()
	}
	return exitCode, stderr
}

func TestVerifyTestMain(t *testing.T) {
	defer clearOSStubs()
	exitCode, stderr := osStubs()

	blocked := startBlockedG()
	VerifyTestMain(dummyTestMain(7))
	assert.Equal(t, 7, <-exitCode, "Exit code should not be modified")
	assert.NotContains(t, <-stderr, "goleak: Errors", "Ignore leaks on unsuccessful runs")

	VerifyTestMain(dummyTestMain(0))
	assert.Equal(t, 1, <-exitCode, "Expect error due to leaks on successful runs")
	assert.Contains(t, <-stderr, "goleak: Errors", "Find leaks on successful runs")

	blocked.unblock()
	VerifyTestMain(dummyTestMain(0))
	assert.Equal(t, 0, <-exitCode, "Expect no errors without leaks")
	assert.NotContains(t, <-stderr, "goleak: Errors", "No errors on successful run without leaks")

	cleanupCalled := false
	cleanupExitcode := 0
	VerifyTestMain(dummyTestMain(3), Cleanup(func(ec int) {
		cleanupCalled = true
		cleanupExitcode = ec
	}))
	assert.True(t, cleanupCalled)
	assert.Equal(t, 3, cleanupExitcode)
}
