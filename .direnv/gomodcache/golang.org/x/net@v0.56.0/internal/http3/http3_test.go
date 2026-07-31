// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http3

import (
	"encoding/hex"
	"strings"
	"testing"
	"testing/synctest"
)

func unhex(s string) []byte {
	b, err := hex.DecodeString(strings.Map(func(c rune) rune {
		switch c {
		case ' ', '\t', '\n':
			return -1 // ignore
		}
		return c
	}, s))
	if err != nil {
		panic(err)
	}
	return b
}

// testReader implements io.Reader.
type testReader struct {
	readFunc func([]byte) (int, error)
}

func (r testReader) Read(p []byte) (n int, err error) { return r.readFunc(p) }

// synctestSubtest runs f in a subtest in a synctest.Run bubble.
func synctestSubtest(t *testing.T, name string, f func(t *testing.T)) {
	t.Run(name, func(t *testing.T) {
		synctest.Test(t, f)
	})
}
