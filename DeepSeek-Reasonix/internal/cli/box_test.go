package cli

import (
	"strings"
	"testing"
)

// --- visibleWidth ---

func TestVisibleWidthPlain(t *testing.T) {
	if got := visibleWidth("hello"); got != 5 {
		t.Errorf("visibleWidth(hello) = %d, want 5", got)
	}
}

func TestVisibleWidthEmpty(t *testing.T) {
	if got := visibleWidth(""); got != 0 {
		t.Errorf("visibleWidth(\"\") = %d, want 0", got)
	}
}

func TestVisibleWidthANSI(t *testing.T) {
	// ANSI SGR codes should not count toward width.
	colored := "\x1b[31mhello\x1b[0m"
	if got := visibleWidth(colored); got != 5 {
		t.Errorf("visibleWidth(colored) = %d, want 5", got)
	}
}

// --- padRight ---

func TestPadRightAlreadyWide(t *testing.T) {
	got := padRight("hello", 3)
	if got != "hello" {
		t.Errorf("padRight(hello, 3) = %q, want hello", got)
	}
}

func TestPadRightExact(t *testing.T) {
	got := padRight("hello", 5)
	if got != "hello" {
		t.Errorf("padRight(hello, 5) = %q, want hello", got)
	}
}

func TestPadRightPads(t *testing.T) {
	got := padRight("hi", 5)
	if got != "hi   " {
		t.Errorf("padRight(hi, 5) = %q, want %q", got, "hi   ")
	}
}

func TestPadRightEmpty(t *testing.T) {
	got := padRight("", 3)
	if got != "   " {
		t.Errorf("padRight(\"\", 3) = %q, want %q", got, "   ")
	}
}

// --- boxed ---

func TestBoxedSingleLine(t *testing.T) {
	got := boxed([]string{"hello"})
	// Should contain the content and the box characters.
	if len(got) == 0 {
		t.Error("boxed should not be empty")
	}
	// Should end with newline.
	if got[len(got)-1] != '\n' {
		t.Error("boxed should end with newline")
	}
}

func TestBoxedMultipleLines(t *testing.T) {
	got := boxed([]string{"line1", "longer line", "short"})
	if len(got) == 0 {
		t.Error("boxed should not be empty")
	}
	// All lines should be present.
	for _, want := range []string{"line1", "longer line", "short"} {
		if !strings.Contains(got, want) {
			t.Errorf("boxed missing %q", want)
		}
	}
}

func TestBoxedEmpty(t *testing.T) {
	got := boxed([]string{})
	if len(got) == 0 {
		t.Error("boxed empty should still produce a box")
	}
}
