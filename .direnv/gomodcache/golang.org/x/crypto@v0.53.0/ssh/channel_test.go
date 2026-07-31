// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"math/bits"
	"testing"
	"time"
	"unsafe"
)

func TestMinPayloadSize(t *testing.T) {
	// 4 GiB (2^32). Declared as a var (not a const) so that int(bigPayload)
	// is a runtime conversion: a constant conversion would fail to compile
	// on 32-bit platforms with "constant 4294967296 overflows int". On
	// 32-bit the value truncates to 0 at runtime, but the is64Bit cases
	// that reference it are skipped by the runtime check below.
	var bigPayload int64 = 1 << 32

	tests := []struct {
		name       string
		maxPayload uint32
		dataLen    int
		want       uint32
		is64Bit    bool // Flag to run only on 64-bit architectures
	}{
		{
			name:       "Normal Case - Data fits in payload",
			maxPayload: 32768,
			dataLen:    1000,
			want:       1000,
		},
		{
			name:       "Normal Case - Data larger than payload",
			maxPayload: 32768,
			dataLen:    50000,
			want:       32768,
		},
		{
			name:       "Boundary Case - Data zero",
			maxPayload: 32768,
			dataLen:    0,
			want:       0,
		},
		{
			name:       "Overflow Case - Data is exactly 4GB (1<<32)",
			maxPayload: 32768,
			dataLen:    int(bigPayload),
			want:       32768,
			is64Bit:    true,
		},
		{
			name:       "Overflow Case - Data is 4GB + small amount",
			maxPayload: 32768,
			dataLen:    int(bigPayload + 100),
			want:       32768,
			is64Bit:    true,
		},
	}

	is64Bit := bits.UintSize == 64

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.is64Bit && !is64Bit {
				t.Skip("Skipping test requiring 64-bit int")
			}
			got := minPayloadSize(tt.maxPayload, tt.dataLen)
			if got != tt.want {
				t.Errorf("minPayloadSize(%d, %d) = %d; want %d", tt.maxPayload, tt.dataLen, got, tt.want)
			}
		})
	}
}

// TestWriteExtendedNoInfiniteLoopOnLargeWrite is an end-to-end regression
// test for the integer-overflow bug in WriteExtended. Before the fix, a
// write whose len(data) was a multiple of 2^32 caused minPayloadSize to
// return 0; WriteExtended then spun forever, reserving 0 bytes per
// iteration and never advancing the data slice.
//
// We exercise the real WriteExtended path with a slice whose declared
// length is exactly 2^32. Allocating 4 GiB is unnecessary: each iteration
// only reads up to maxRemotePayload bytes from the head of the slice, and
// the loop blocks in remoteWin.reserve() once the channel window is
// exhausted — before the slice base advances past the underlying buffer.
//
// With the fix, the loop blocks in reserve(); we detect that via
// waitWriterBlocked(), then close the window to let WriteExtended return.
// With the bug, the loop never blocks and the test times out.
//
//go:nocheckptr
func TestWriteExtendedNoInfiniteLoopOnLargeWrite(t *testing.T) {
	if bits.UintSize < 64 {
		t.Skip("test requires 64-bit int to construct a slice with len >= 2^32")
	}

	reader, writer, mux := channelPair(t)
	defer reader.Close()
	defer writer.Close()
	defer mux.Close()

	// Sized to hold the full pre-update remote window so that no iteration
	// reads past the backing buffer before reserve() blocks.
	backing := make([]byte, channelWindowSize)
	var bigLen int64 = 1 << 32
	bigSlice := unsafe.Slice(&backing[0], int(bigLen))

	done := make(chan int, 1)
	go func() {
		n, _ := writer.Write(bigSlice)
		done <- n
	}()

	blocked := make(chan struct{})
	go func() {
		writer.remoteWin.waitWriterBlocked()
		close(blocked)
	}()

	select {
	case <-blocked:
		// Good — the loop made progress and is now blocked in reserve().
		// Close the window to let WriteExtended return.
		writer.remoteWin.close()
	case <-time.After(2 * time.Second):
		t.Fatal("WriteExtended did not block in reserve within 2s — minPayloadSize likely returned 0 (integer overflow regression)")
	}

	select {
	case n := <-done:
		if n == 0 {
			t.Fatalf("WriteExtended returned n=0; expected progress")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteExtended did not return after closing the window")
	}
}
