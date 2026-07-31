// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !go1.26

// Go 1.25 and earlier: No http.ClientConn support.

package http2_test

import (
	"net/http"
	"testing"
)

type httpClientConn struct{} // http.ClientConn was added in Go 1.26

func (httpClientConn) RoundTrip(*http.Request) (*http.Response, error) {
	panic("should never be called")
}

func newTestClientConn(t testing.TB, opts ...any) *testClientConn {
	t.Helper()

	tt := newTestTransport(t, opts...)

	if tt.mode == roundTripNetHTTP {
		t.Skip("roundTripNetHTTP not supported go <1.26: no NewClientConn")
	}

	nc := tt.li.newConn()
	const singleUse = false
	_, err := tt.tr.TestNewClientConn(nc, singleUse, nil)
	if err != nil {
		t.Fatalf("newClientConn: %v", err)
	}

	return tt.getConn()
}

func (tc *testClientConn) doRoundTrip(req *http.Request, f func(streamID uint32)) (*http.Response, error) {
	return tc.cc.TestRoundTrip(req, f)
}
