// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tiff

import (
	"fmt"
	"io"
	"math"
	"slices"
)

// buffer buffers an io.Reader to satisfy io.ReaderAt.
type buffer struct {
	r   io.Reader
	buf []byte
}

const fillChunkSize = 10 << 20 // 10 MB

// fill reads data from b.r until the buffer contains at least end bytes.
func (b *buffer) fill(end int) error {
	m := len(b.buf)
	for m < end {
		next := min(end-m, fillChunkSize)
		b.buf = slices.Grow(b.buf, next)
		b.buf = b.buf[:m+next]
		n, err := io.ReadFull(b.r, b.buf[m:m+next])
		m += n
		b.buf = b.buf[:m]
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *buffer) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		// Impossible in correct usage, but check for safety.
		return 0, fmt.Errorf("invalid ReadAt offset %v (bug)", off)
	}
	end64 := off + int64(len(p))
	if end64 < off || end64 > math.MaxInt {
		return 0, io.ErrUnexpectedEOF
	}
	end := int(end64)

	err := b.fill(end)
	end = min(end, len(b.buf))
	return copy(p, b.buf[min(int(off), end):end]), err
}

// Slice returns a slice of the underlying buffer. The slice contains
// n bytes starting at offset off.
func (b *buffer) Slice(off, n int64) ([]byte, error) {
	if off < 0 || n < 0 {
		// Impossible in correct usage, but check for safety.
		return nil, fmt.Errorf("invalid negative input to Slice(%v, %v) (bug)", off, n)
	}
	end := off + n
	if end < 0 || end > math.MaxInt {
		// end is too large. Treat this as a read error.
		return nil, io.ErrUnexpectedEOF
	}
	if err := b.fill(int(end)); err != nil {
		return nil, err
	}
	return b.buf[off:end], nil
}

// newReaderAt converts an io.Reader into an io.ReaderAt.
func newReaderAt(r io.Reader) io.ReaderAt {
	if ra, ok := r.(io.ReaderAt); ok {
		return ra
	}
	return &buffer{
		r:   r,
		buf: make([]byte, 0, 1024),
	}
}
