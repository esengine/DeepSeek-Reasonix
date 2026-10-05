package delta

import (
	"errors"
	"io"
)

// Chunk boundaries are part of the format: the builder and every client must
// cut the same bytes at the same places, or nothing an install holds is found
// again. Changing any of these, or the gear table, is a new SchemaVersion.
const (
	minChunk = 4 << 10
	avgChunk = 16 << 10
	maxChunk = 64 << 10
	// Normalized chunking: a stricter mask before the average size and a looser
	// one after it pulls chunk sizes toward the average.
	maskStrict = (1 << 16) - 1
	maskLoose  = (1 << 12) - 1
)

// gear is the rolling hash's byte table, fixed by a deterministic generator so
// that it is reproducible from this file alone.
var gear = func() (g [256]uint64) {
	x := uint64(0x9E3779B97F4A7C15)
	for i := range g {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		g[i] = x
	}
	return
}()

// Span is one chunk: its offset in the file and its length.
type Span struct {
	Off int64
	Len int
}

// Split cuts data into content-defined chunks. An empty input has no chunks.
func Split(data []byte) []Span {
	var out []Span
	for start := 0; start < len(data); {
		n := cut(data[start:])
		out = append(out, Span{Off: int64(start), Len: n})
		start += n
	}
	return out
}

// cut is the length of the chunk that begins data.
func cut(data []byte) int {
	n := len(data)
	if n <= minChunk {
		return n
	}
	if n > maxChunk {
		n = maxChunk
	}
	mid := min(avgChunk, n)
	var h uint64
	i := minChunk
	for ; i < mid; i++ {
		h = (h << 1) + gear[data[i]]
		if h&maskStrict == 0 {
			return i + 1
		}
	}
	for ; i < n; i++ {
		h = (h << 1) + gear[data[i]]
		if h&maskLoose == 0 {
			return i + 1
		}
	}
	return n
}

// SplitReader cuts a stream into the same chunks Split cuts its bytes into,
// holding at most two chunks of it in memory. fn's data is only valid for the
// duration of the call.
func SplitReader(r io.Reader, fn func(Span, []byte) error) error {
	buf := make([]byte, 0, 2*maxChunk)
	var off int64
	eof := false
	for {
		for !eof && len(buf) < maxChunk {
			n, err := r.Read(buf[len(buf):cap(buf)])
			buf = buf[:len(buf)+n]
			if errors.Is(err, io.EOF) {
				eof = true
			} else if err != nil {
				return err
			}
		}
		if len(buf) == 0 {
			return nil
		}
		n := cut(buf)
		if err := fn(Span{Off: off, Len: n}, buf[:n]); err != nil {
			return err
		}
		off += int64(n)
		buf = buf[:copy(buf, buf[n:])]
	}
}
