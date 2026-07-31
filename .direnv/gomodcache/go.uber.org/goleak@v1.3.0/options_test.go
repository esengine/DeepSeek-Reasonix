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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak/internal/stack"
)

func TestOptionsFilters(t *testing.T) {
	opts := buildOpts()
	cur := stack.Current()
	all := getStableAll(t, cur)

	// At least one of these should be the same as current, the others should be filtered out.
	for _, s := range all {
		if s.ID() == cur.ID() {
			require.False(t, opts.filter(s), "Current test running function should not be filtered")
		} else {
			require.True(t, opts.filter(s), "Default goroutines should be filtered: %v", s)
		}
	}

	defer startBlockedG().unblock()

	// Now the filters should find something that doesn't match a filter.
	countUnfiltered := func() int {
		var unmatched int
		for _, s := range stack.All() {
			if s.ID() == cur.ID() {
				continue
			}
			if !opts.filter(s) {
				unmatched++
			}
		}
		return unmatched
	}
	require.Equal(t, 1, countUnfiltered(), "Expected blockedG goroutine to not match any filter")

	// If we add an extra filter to ignore blockTill, it shouldn't match.
	opts = buildOpts(IgnoreTopFunction("go.uber.org/goleak.(*blockedG).block"))
	require.Zero(t, countUnfiltered(), "blockedG should be filtered out. running: %v", stack.All())

	// If we ignore startBlockedG, that should not ignore the blockedG goroutine
	// because startBlockedG should be the "created by" function in the stack.
	opts = buildOpts(IgnoreAnyFunction("go.uber.org/goleak.startBlockedG"))
	require.Equal(t, 1, countUnfiltered(),
		"startBlockedG should not be filtered out. running: %v", stack.All())
}

func TestOptionsIgnoreAnyFunction(t *testing.T) {
	cur := stack.Current()
	opts := buildOpts(IgnoreAnyFunction("go.uber.org/goleak.(*blockedG).run"))

	for _, s := range stack.All() {
		if s.ID() == cur.ID() {
			continue
		}

		if opts.filter(s) {
			continue
		}

		t.Errorf("Unexpected goroutine: %v", s)
	}
}

func TestOptionsRetry(t *testing.T) {
	opts := buildOpts()
	opts.maxRetries = 50 // initial attempt + 50 retries = 11
	opts.maxSleep = time.Millisecond

	for i := 0; i < 50; i++ {
		assert.True(t, opts.retry(i), "Attempt %v/51 should allow retrying", i)
	}
	assert.False(t, opts.retry(51), "Attempt 51/51 should not allow retrying")
	assert.False(t, opts.retry(52), "Attempt 52/51 should not allow retrying")
}
