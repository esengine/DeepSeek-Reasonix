//go:build windows && !appengine
// +build windows,!appengine

package colorable

import (
	"io"
	"testing"
)

func BenchmarkWriterPlaintext(b *testing.B) {
	w := &writer{out: io.Discard}
	data := []byte("hello world\n")
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		w.Write(data)
	}
}

func BenchmarkWriterPlaintextLarge(b *testing.B) {
	w := &writer{out: io.Discard}
	data := make([]byte, 4096)
	for i := range data {
		data[i] = 'x'
	}
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		w.Write(data)
	}
}
