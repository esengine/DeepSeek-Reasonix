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

package goleak_test

// Importing the os/signal package causes a goroutine to be started.
import (
	"os"
	"os/signal"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestNoLeaks(t *testing.T) {
	// Just importing the package can cause leaks.
	require.NoError(t, goleak.Find(), "Found leaks caused by signal import")

	// Register some signal handlers and ensure there's no leaks.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	require.NoError(t, goleak.Find(), "Found leaks caused by signal.Notify")

	// Restore all registered signals.
	signal.Reset(os.Interrupt)
	require.NoError(t, goleak.Find(), "Found leaks caused after signal.Reset")
}
