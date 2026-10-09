package textutil

import "testing"

func TestClipGraphemesCountsSuffixInsideBudget(t *testing.T) {
	cluster := "👨‍👩‍👧‍👦"
	got := ClipGraphemes("a"+cluster+"bc", 3, "…")
	want := "a" + cluster + "…"
	if got != want {
		t.Fatalf("ClipGraphemes() = %q, want %q", got, want)
	}
	if got := ClipGraphemes("abc", 1, "…"); got != "a" {
		t.Fatalf("ClipGraphemes() = %q, want first grapheme without suffix", got)
	}
}

func TestTruncateGraphemesAppendsSuffixOutsideBudget(t *testing.T) {
	cluster := "👨‍👩‍👧‍👦"
	got := TruncateGraphemes("a"+cluster+"bc", 2, "...")
	want := "a" + cluster + "..."
	if got != want {
		t.Fatalf("TruncateGraphemes() = %q, want %q", got, want)
	}
}

func TestHiddenControlsAreFoundAndStripped(t *testing.T) {
	for _, s := range []string{"a\x1b]8;;http://x\x07b", "a‮b", "a\x00b", "a\u0085b", "a⁦b"} {
		if !HasHiddenControls(s) || HasHiddenControls(StripHiddenControls(s)) {
			t.Errorf("%q not handled", s)
		}
	}
	if HasHiddenControls("line one\n\tline two ✓") {
		t.Error("newline and tab are plain text")
	}
	if got := StripHiddenControls("a\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\b"); got != "alinkb" {
		t.Errorf("got %q", got)
	}
}
