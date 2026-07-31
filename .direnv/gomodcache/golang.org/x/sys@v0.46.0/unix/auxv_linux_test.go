// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.21 && linux

package unix_test

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestAuxv(t *testing.T) {
	vec, err := unix.Auxv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) == 0 {
		t.Errorf("got zero auxv entries on linux, expected > 0")
	}
}
