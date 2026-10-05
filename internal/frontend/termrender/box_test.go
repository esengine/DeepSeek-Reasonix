package termrender

import (
	"strings"
	"testing"
)

// VisibleWidth

func TestVisibleWidthPlain(t *testing.T) {
	if got := VisibleWidth("hello"); got != 5 {
		t.Errorf("VisibleWidth(hello) = %d, want 5", got)
	}
}

func TestVisibleWidthEmpty(t *testing.T) {
	if got := VisibleWidth(""); got != 0 {
		t.Errorf("VisibleWidth(\"\") = %d, want 0", got)
	}
}

func TestVisibleWidthANSI(t *testing.T) {
	// ANSI SGR codes should not count toward width.
	colored := "\x1b[31mhello\x1b[0m"
	if got := VisibleWidth(colored); got != 5 {
		t.Errorf("VisibleWidth(colored) = %d, want 5", got)
	}
}

// PadRight

func TestPadRightAlreadyWide(t *testing.T) {
	got := PadRight("hello", 3)
	if got != "hello" {
		t.Errorf("PadRight(hello, 3) = %q, want hello", got)
	}
}

func TestPadRightExact(t *testing.T) {
	got := PadRight("hello", 5)
	if got != "hello" {
		t.Errorf("PadRight(hello, 5) = %q, want hello", got)
	}
}

func TestPadRightPads(t *testing.T) {
	got := PadRight("hi", 5)
	if got != "hi   " {
		t.Errorf("PadRight(hi, 5) = %q, want %q", got, "hi   ")
	}
}

func TestPadRightEmpty(t *testing.T) {
	got := PadRight("", 3)
	if got != "   " {
		t.Errorf("PadRight(\"\", 3) = %q, want %q", got, "   ")
	}
}

// Boxed

func TestBoxedSingleLine(t *testing.T) {
	got := Boxed([]string{"hello"})
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
	got := Boxed([]string{"line1", "longer line", "short"})
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
	got := Boxed([]string{})
	if len(got) == 0 {
		t.Error("boxed empty should still produce a box")
	}
}
