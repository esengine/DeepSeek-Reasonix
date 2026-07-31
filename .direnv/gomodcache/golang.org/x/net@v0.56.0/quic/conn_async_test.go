// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"context"
	"errors"
	"fmt"
	"testing/synctest"
)

// An asyncOp is an asynchronous operation that results in (T, error).
type asyncOp[T any] struct {
	v          T
	err        error
	donec      chan struct{}
	cancelFunc context.CancelFunc
}

// cancel cancels the async operation's context, and waits for
// the operation to complete.
func (a *asyncOp[T]) cancel() {
	synctest.Wait()
	select {
	case <-a.donec:
		return // already done
	default:
	}
	a.cancelFunc()
	synctest.Wait()
	select {
	case <-a.donec:
	default:
		panic(fmt.Errorf("async op failed to finish after being canceled"))
	}
}

var errNotDone = errors.New("async op is not done")

// result returns the result of the async operation.
// It returns errNotDone if the operation is still in progress.
//
// Note that unlike a traditional async/await, this doesn't block
// waiting for the operation to complete. Since tests have full
// control over the progress of operations, an asyncOp can only
// become done in reaction to the test taking some action.
func (a *asyncOp[T]) result() (v T, err error) {
	synctest.Wait()
	select {
	case <-a.donec:
		return a.v, a.err
	default:
		return a.v, errNotDone
	}
}

// runAsync starts an asynchronous operation.
//
// The function f should call a blocking function such as
// Stream.Write or Conn.AcceptStream and return its result.
// It must use the provided context.
func runAsync[T any](tc *testConn, f func(context.Context) (T, error)) *asyncOp[T] {
	ctx, cancel := context.WithCancel(tc.t.Context())
	a := &asyncOp[T]{
		donec:      make(chan struct{}),
		cancelFunc: cancel,
	}
	go func() {
		defer close(a.donec)
		a.v, a.err = f(ctx)
	}()
	synctest.Wait()
	return a
}
