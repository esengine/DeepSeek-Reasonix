package cosplay

import (
	"strings"
	"testing"
)

func TestLimitedBufferCapsOutput(t *testing.T) {
	var b limitedBuffer
	b.max = 16
	big := strings.Repeat("x", 100)
	if _, err := b.Write([]byte(big)); err != nil {
		t.Fatal(err)
	}
	if b.buf.Len() != 16 {
		t.Errorf("captured %d bytes, want 16", b.buf.Len())
	}
	// Further writes are dropped entirely.
	if _, err := b.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if b.buf.Len() != 16 {
		t.Errorf("after overflow captured %d bytes, want still 16", b.buf.Len())
	}
}

func TestLimitedBufferPartialFill(t *testing.T) {
	var b limitedBuffer
	b.max = 10
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("world!")); err != nil { // 11 bytes, cap 10
		t.Fatal(err)
	}
	if got := b.buf.String(); got != "helloworld" {
		t.Errorf("partial fill = %q, want %q", got, "helloworld")
	}
}
