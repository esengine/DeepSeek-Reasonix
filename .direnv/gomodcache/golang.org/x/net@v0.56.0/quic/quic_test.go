// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"testing"
	"testing/synctest"
)

func testSides(t *testing.T, name string, f func(*testing.T, connSide)) {
	if name != "" {
		name += "/"
	}
	t.Run(name+"server", func(t *testing.T) { f(t, serverSide) })
	t.Run(name+"client", func(t *testing.T) { f(t, clientSide) })
}

func testSidesSynctest(t *testing.T, name string, f func(*testing.T, connSide)) {
	t.Helper()
	testSides(t, name, func(t *testing.T, side connSide) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			f(t, side)
		})
	})
}

func testStreamTypes(t *testing.T, name string, f func(*testing.T, streamType)) {
	if name != "" {
		name += "/"
	}
	t.Run(name+"bidi", func(t *testing.T) { f(t, bidiStream) })
	t.Run(name+"uni", func(t *testing.T) { f(t, uniStream) })
}

func testStreamTypesSynctest(t *testing.T, name string, f func(*testing.T, streamType)) {
	t.Helper()
	testStreamTypes(t, name, func(t *testing.T, stype streamType) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			f(t, stype)
		})
	})
}

func testSidesAndStreamTypes(t *testing.T, name string, f func(*testing.T, connSide, streamType)) {
	if name != "" {
		name += "/"
	}
	t.Run(name+"server/bidi", func(t *testing.T) { f(t, serverSide, bidiStream) })
	t.Run(name+"client/bidi", func(t *testing.T) { f(t, clientSide, bidiStream) })
	t.Run(name+"server/uni", func(t *testing.T) { f(t, serverSide, uniStream) })
	t.Run(name+"client/uni", func(t *testing.T) { f(t, clientSide, uniStream) })
}

func testSidesAndStreamTypesSynctest(t *testing.T, name string, f func(*testing.T, connSide, streamType)) {
	t.Helper()
	testSidesAndStreamTypes(t, name, func(t *testing.T, side connSide, stype streamType) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			f(t, side, stype)
		})
	})
}

func synctestSubtest(t *testing.T, name string, f func(t *testing.T)) {
	t.Run(name, func(t *testing.T) {
		t.Helper()
		synctest.Test(t, f)
	})
}
