package builtin

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// recordingEmit collects every emitted chunk in order so a test can assert on
// both the per-chunk boundaries and the reassembled stream.
func recordingEmit() (func(string), func() string) {
	var b strings.Builder
	fn := func(chunk string) { b.WriteString(chunk) }
	return fn, b.String
}

// "中" is U+4E2D = E4 B8 AD in UTF-8; splitting it across two Write calls is the
// core regression: without byte-boundary reassembly each half is emitted as a
// standalone (invalid) string and JSON-encoded into U+FFFD before the other
// half arrives.
func TestProgressWriterReassemblesSplitMultibyteRune(t *testing.T) {
	emit, result := recordingEmit()
	w := &progressWriter{emit: emit}

	if n, err := w.Write([]byte{0xE4, 0xB8}); err != nil || n != 2 {
		t.Fatalf("first write: n=%d err=%v, want n=2 err=nil", n, err)
	}
	// The incomplete rune must be held back, not flushed as invalid UTF-8.
	if result() != "" {
		t.Fatalf("after partial rune emitted %q; want it held back", result())
	}
	if _, err := w.Write([]byte{0xAD}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got := result(); got != "中" {
		t.Fatalf("reassembled %q, want %q", got, "中")
	}
}

func TestProgressWriterEmitsCompleteRunesImmediately(t *testing.T) {
	emit, result := recordingEmit()
	w := &progressWriter{emit: emit}
	if _, err := w.Write([]byte("ok 日本")); err != nil {
		t.Fatal(err)
	}
	if got := result(); got != "ok 日本" {
		t.Fatalf("got %q, want %q", got, "ok 日本")
	}
}

func TestProgressWriterReassemblesEverySplitPosition(t *testing.T) {
	orig := "你好世界🎉"
	emit, result := recordingEmit()
	w := &progressWriter{emit: emit}
	// One byte at a time exercises every possible split inside a rune.
	for _, b := range []byte(orig) {
		if _, err := w.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
		// At no intermediate point may the emitted stream be invalid UTF-8.
		if !utf8.ValidString(result()) {
			t.Fatalf("emitted invalid UTF-8 mid-stream: %q", result())
		}
	}
	if got := result(); got != orig {
		t.Fatalf("reassembled %q, want %q", got, orig)
	}
}

func TestProgressWriterReportsFullLengthWhileHoldingBack(t *testing.T) {
	w := &progressWriter{emit: func(string) {}}
	// Write must report the full byte count even when it buffers the tail, or
	// the io copy driving the pipe would short-read / block.
	if n, err := w.Write([]byte{0xE4, 0xB8}); err != nil || n != 2 {
		t.Fatalf("got n=%d err=%v, want n=2 err=nil", n, err)
	}
}

func TestProgressWriterDoesNotStallOnInvalidByte(t *testing.T) {
	var emitted bytes.Buffer
	w := &progressWriter{emit: func(s string) { emitted.WriteString(s) }}
	// A stray invalid byte (0xFF) has no valid completion; it must be emitted
	// rather than buffered forever, so subsequent output is not swallowed.
	if _, err := w.Write([]byte{0xFF}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	if emitted.Len() == 0 || !strings.HasSuffix(emitted.String(), "tail") {
		t.Fatalf("got %q; invalid byte must not stall later output", emitted.String())
	}
}
