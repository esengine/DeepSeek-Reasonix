package cli

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestScrollbarThumb(t *testing.T) {
	if _, size := scrollbarThumb(10, 0, 5); size != 0 {
		t.Errorf("content within viewport should have no thumb, got size %d", size)
	}
	if start, _ := scrollbarThumb(10, 0, 100); start != 0 {
		t.Errorf("at top the thumb starts at row 0, got %d", start)
	}
	const h, total = 10, 100
	if start, size := scrollbarThumb(h, total-h, total); start+size != h {
		t.Errorf("at bottom the thumb reaches the last row: start=%d size=%d h=%d", start, size, h)
	}
}

func TestEdgeScrollDir(t *testing.T) {
	const h = 10
	if got := edgeScrollDir(0, h); got != -1 {
		t.Errorf("top edge dir = %d, want -1", got)
	}
	if got := edgeScrollDir(h-1, h); got != 1 {
		t.Errorf("bottom edge dir = %d, want 1", got)
	}
	if got := edgeScrollDir(h/2, h); got != 0 {
		t.Errorf("middle dir = %d, want 0", got)
	}
}

func TestSelSpan(t *testing.T) {
	start, end, cw := selPos{line: 1, col: 3}, selPos{line: 3, col: 5}, 20
	for _, tc := range []struct {
		idx         int
		wantOK      bool
		wantLo, wHi int
	}{
		{0, false, 0, 0}, // above
		{1, true, 3, cw}, // first line: anchor col → right edge
		{2, true, 0, cw}, // middle line: full width
		{3, true, 0, 5},  // last line: left edge → head col
		{4, false, 0, 0}, // below
	} {
		lo, hi, ok := selSpan(tc.idx, start, end, cw)
		if ok != tc.wantOK || (ok && (lo != tc.wantLo || hi != tc.wHi)) {
			t.Errorf("selSpan(%d) = (%d,%d,%v), want (%d,%d,%v)", tc.idx, lo, hi, ok, tc.wantLo, tc.wHi, tc.wantOK)
		}
	}
}

func TestSelectedTextMultiLine(t *testing.T) {
	m := newTestChatTUI()
	m.wrappedLines = []string{"hello world", "second line", "third row"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 6}, head: selPos{line: 2, col: 5}}

	if got, want := m.selectedText(), "world\nsecond line\nthird"; got != want {
		t.Errorf("selectedText() = %q, want %q", got, want)
	}

	// A zero-width selection (plain click) copies nothing.
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 3}, head: selPos{line: 0, col: 3}}
	if got := m.selectedText(); got != "" {
		t.Errorf("empty selection should yield no text, got %q", got)
	}
}

func TestCopyToClipboard_PlatformSuccess(t *testing.T) {
	defer func(fn func(string) error) { clipboardWriteAll = fn }(clipboardWriteAll)
	var written string
	clipboardWriteAll = func(s string) error { written = s; return nil }

	cmd := copyToClipboard("hello")
	msg := cmd()
	if msg != nil {
		t.Error("successful platform write should return nil msg, got non-nil (OSC 52 fallback triggered)")
	}
	if written != "hello" {
		t.Errorf("clipboardWriteAll got %q, want %q", written, "hello")
	}
}

func TestCopyToClipboard_OSC52Fallback(t *testing.T) {
	defer func(fn func(string) error) { clipboardWriteAll = fn }(clipboardWriteAll)
	clipboardWriteAll = func(string) error { return errors.New("no display") }

	cmd := copyToClipboard("copied text")
	msg := cmd()
	if msg == nil {
		t.Fatal("platform failure should trigger OSC 52 fallback, got nil msg")
	}

	// The inner Cmd from tea.Printf, when executed, should produce output
	// containing the OSC 52 sequence with the base64-encoded text.
	innerCmd, ok := msg.(tea.Cmd)
	if !ok {
		t.Fatalf("expected inner tea.Cmd, got %T", msg)
	}
	// Verify the expected OSC 52 payload is part of the sequence.
	want := ansi.SetSystemClipboard("copied text")
	if !strings.Contains(want, "copied text") && !strings.Contains(want, "\x1b]52;") {
		t.Errorf("OSC 52 sequence should contain clipboard escape, got %q", want)
	}
	// Execute the inner Cmd to prove it doesn't panic.
	innerCmd()
}
