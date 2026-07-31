// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build netbsd

package unix_test

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGetvfsstat(t *testing.T) {
	n, err := unix.Getvfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		t.Fatal(err)
	}

	data := make([]unix.Statvfs_t, n)
	n2, err := unix.Getvfsstat(data, unix.MNT_NOWAIT)
	if err != nil {
		t.Fatal(err)
	}
	if n != n2 {
		t.Errorf("Getvfsstat(nil) = %d, but subsequent Getvfsstat(slice) = %d", n, n2)
	}
	for i, stat := range data {
		if stat == (unix.Statvfs_t{}) {
			t.Errorf("index %v is an empty Statvfs_t struct", i)
		}
		t.Logf("%s on %s", unix.ByteSliceToString(stat.Mntfromname[:]), unix.ByteSliceToString(stat.Mntonname[:]))
	}
	if t.Failed() {
		for i, stat := range data[:n2] {
			t.Logf("data[%v] = %+v", i, stat)
		}
		mount, err := exec.Command("mount").CombinedOutput()
		if err != nil {
			t.Logf("mount: %v\n%s", err, mount)
		} else {
			t.Logf("mount: %s", mount)
		}
	}
}
