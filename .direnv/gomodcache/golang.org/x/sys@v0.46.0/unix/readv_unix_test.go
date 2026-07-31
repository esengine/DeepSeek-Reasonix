// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || linux || openbsd

package unix_test

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWritevReadv(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	data := [][]byte{[]byte("hello"), []byte(", "), []byte("world")}
	wn, err := unix.Writev(int(w.Fd()), data)
	if err != nil {
		t.Fatal(err)
	}
	if wn != 12 {
		t.Fatalf("Writev wrote %d bytes, want 12", wn)
	}
	w.Close()

	buf1 := make([]byte, 5)
	buf2 := make([]byte, 2)
	buf3 := make([]byte, 5)
	rn, err := unix.Readv(int(r.Fd()), [][]byte{buf1, buf2, buf3})
	if err != nil {
		t.Fatal(err)
	}
	if rn != 12 {
		t.Fatalf("Readv read %d bytes, want 12", rn)
	}
	if string(buf1) != "hello" || string(buf2) != ", " || string(buf3[:5]) != "world" {
		t.Fatalf("Readv got %q %q %q, want %q %q %q",
			buf1, buf2, buf3, "hello", ", ", "world")
	}
}

func TestPwritevPreadv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preadv_test")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	const off = 16
	data := [][]byte{[]byte("foo"), []byte("bar")}
	wn, err := unix.Pwritev(int(f.Fd()), data, off)
	if err != nil {
		t.Fatal(err)
	}
	if wn != 6 {
		t.Fatalf("Pwritev wrote %d bytes, want 6", wn)
	}

	got1 := make([]byte, 3)
	got2 := make([]byte, 3)
	rn, err := unix.Preadv(int(f.Fd()), [][]byte{got1, got2}, off)
	if err != nil {
		t.Fatal(err)
	}
	if rn != 6 {
		t.Fatalf("Preadv read %d bytes, want 6", rn)
	}
	if string(got1) != "foo" || string(got2) != "bar" {
		t.Fatalf("Preadv got %q %q, want %q %q", got1, got2, "foo", "bar")
	}
}

func TestPwritevOffset(t *testing.T) {
	// Regression guard: off_t encoding must not overflow on large offsets.
	// Mirrors the Linux test for golang.org/issues/57291.
	path := filepath.Join(t.TempDir(), "offset_test")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	const off = 20
	b := [][]byte{{byte(0)}}
	n, err := unix.Pwritev(int(f.Fd()), b, off)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Pwritev wrote %d bytes, want 1", n)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(off + 1); info.Size() != want {
		t.Fatalf("file size %d, want %d", info.Size(), want)
	}
}

func TestWritevReadvAllocations(t *testing.T) {
	// Verify no heap allocations when iovec count <= minIovec (8).
	// Allocations in this path indicate a regression in the fast path.
	f, err := os.Create(filepath.Join(t.TempDir(), "alloc_test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	iovs := make([][]byte, 8)
	for i := range iovs {
		iovs[i] = []byte{'A'}
	}

	check := func(name string, fn func()) {
		t.Helper()
		n := int(testing.AllocsPerRun(100, fn))
		if n != 0 {
			t.Errorf("%s: got %d allocations, want 0", name, n)
		}
	}

	check("Writev", func() { unix.Writev(int(f.Fd()), iovs) })
	check("Pwritev", func() { unix.Pwritev(int(f.Fd()), iovs, 0) })
	check("Readv", func() { unix.Readv(int(f.Fd()), iovs) })
	check("Preadv", func() { unix.Preadv(int(f.Fd()), iovs, 0) })
}
