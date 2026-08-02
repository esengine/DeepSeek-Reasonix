package builtin

import (
	"strings"
	"testing"
)

// TestOldStringNotFoundErrorCRLFHintClean proves the not-found error's nearest
// line hint stays a single clean row on CRLF files: no stray "\r" escape and
// no embedded line breaks, so the quoted hint reads like the actual source
// line rather than a mangled fragment.
func TestOldStringNotFoundErrorCRLFHintClean(t *testing.T) {
	content := "def route():\r\n    for layer in layers:\r\n        pass\r\n"
	old := "    for layer in layers:\n"

	err := oldStringNotFoundError("router.py", old, content)
	msg := err.Error()
	// The hint names a real line without a carriage-return escape.
	if !strings.Contains(msg, "nearest line 2:") {
		t.Fatalf("error should point at the nearest line, got: %s", msg)
	}
	if strings.Contains(msg, `\r`) {
		t.Fatalf("error hint leaked a carriage-return escape: %s", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Fatalf("error hint spans multiple lines: %s", msg)
	}
	if !strings.Contains(msg, "for layer in layers:") {
		t.Fatalf("error hint should carry the source line text, got: %s", msg)
	}
	// CRLF-aware hint: normalized LF-only old_string exists in the CRLF file.
	if !strings.Contains(msg, "normally normalize LF-only old_string") {
		t.Fatalf("CRLF hint missing, got: %s", msg)
	}
}

// TestOneLineHintClampsLongRows proves the nearest-line hint is clamped to a
// readable length with an ellipsis.
func TestOneLineHintClampsLongRows(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := oneLineHint(long)
	if len([]rune(got)) > nearestHintMaxRunes {
		t.Fatalf("oneLineHint length = %d, want <= %d", len([]rune(got)), nearestHintMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("clamped hint should end with an ellipsis, got %q", got)
	}
	// Multi-line and whitespace runs collapse to a single row.
	if got := oneLineHint("  a\n\tb  c  "); got != "a b c" {
		t.Fatalf("oneLineHint flatten = %q, want \"a b c\"", got)
	}
	if got := oneLineHint("short"); got != "short" {
		t.Fatalf("oneLineHint pass-through = %q", got)
	}
}

// TestOldStringMatchLineSummaryCarriesContent proves the not-unique error
// lists each duplicate with its row's clamped content AND the following row,
// so the model can distinguish identical duplicates and extend the old_string
// with nearby unique code. CRLF rows must not leak a "\r" escape.
func TestOldStringMatchLineSummaryCarriesContent(t *testing.T) {
	content := "def a():\r\n    return 1\r\n    a_only()\r\n\r\n\ndef b():\r\n    return 1\r\n    b_only()\r\n"
	old := "    return 1\n"
	got := oldStringMatchLineSummary(old, content, 3)
	for _, want := range []string{
		`2: "return 1" (next 3: "a_only()")`,
		`7: "return 1" (next 8: "b_only()")`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, `\r`) {
		t.Fatalf("summary leaked a carriage-return escape: %s", got)
	}
	// A limit caps how many duplicates are listed.
	got = oldStringMatchLineSummary(old, content, 1)
	if !strings.Contains(got, "2: ") || strings.Contains(got, "6: ") {
		t.Fatalf("limited summary = %q, want only the first duplicate", got)
	}
	// No match yields nothing.
	if got := oldStringMatchLineSummary("missing", content, 3); got != "" {
		t.Fatalf("no-match summary = %q, want empty", got)
	}
}
