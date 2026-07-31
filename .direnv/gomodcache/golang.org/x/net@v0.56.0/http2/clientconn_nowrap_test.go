// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !(go1.27 && !http2legacy)

// Using the HTTP/2 implementation in this package:
// Internal package hooks available.

package http2_test

import (
	"testing/synctest"

	"golang.org/x/net/http2"
)

const wrappedAPI = false

func (tt *testTransport) maybeAddNewClientConnHook() {
	tt.tr.TestSetNewClientConnHook(func(cc *http2.ClientConn) {
		nc, ok := cc.TestNetConn().(*synctestNetConn)
		if !ok {
			return
		}
		tt.addPending(nc.peer, cc, nil)
	})
}

// inflowWindow returns the amount of inbound flow control available for a stream,
// or for the connection if streamID is 0.
func (tc *testClientConn) inflowWindow(streamID uint32) int32 {
	synctest.Wait()
	w, err := tc.cc.TestInflowWindow(streamID)
	if err != nil {
		tc.t.Error(err)
	}
	return w
}
