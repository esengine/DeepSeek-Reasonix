// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.27 && !http2legacy

// Wrapping the HTTP/2 implementation in net/http/internal/http2:
// Internal package hooks not available.

package http2_test

import (
	"errors"
	"net/http"
	"testing"
)

const wrappedAPI = true

type httpClientConn = http.ClientConn

func newTestClientConn(t testing.TB, opts ...any) *testClientConn {
	t.Helper()
	tt := newTestTransport(t, opts...)
	cc, err := tt.tr1.NewClientConn(t.Context(), "http", "localhost:80")
	if err != nil {
		t.Fatalf("NewClientConn: %v", err)
	}

	tc := tt.getConn()
	tc.cc1 = cc
	return tc
}

func (tr *testTransport) maybeAddNewClientConnHook() {
}

func (tc *testClientConn) doRoundTrip(req *http.Request, f func(streamID uint32)) (*http.Response, error) {
	tc.t.Helper()
	if tc.cc1 == nil {
		tc.t.Errorf("RoundTrip on testClientConn with no ClientConn (did you mean to use the testTransport?)")
		return nil, errors.New("no ClientConn")
	}
	return tc.cc1.RoundTrip(req)
}
