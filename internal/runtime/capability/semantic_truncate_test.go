package capability

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateCutsOnRuneBoundary(t *testing.T) {
	// One ASCII byte then 50 three-byte characters, so a 120-byte cut lands
	// inside a character.
	desc := "x" + strings.Repeat("审", 50)
	got := truncate(desc, 120)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate(%q, 120) = %q, not valid UTF-8", desc, got)
	}
	want := "x" + strings.Repeat("审", 39) + "…"
	if got != want {
		t.Fatalf("truncate = %q, want %q", got, want)
	}
}

func TestTruncateKeepsASCIICut(t *testing.T) {
	if got := truncate(strings.Repeat("a", 130), 120); got != strings.Repeat("a", 120)+"…" {
		t.Fatalf("truncate ASCII = %q", got)
	}
	if got := truncate("  short  ", 120); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
}
