package textutil

import "testing"

func TestFitGraphemeBytesKeepsCluster(t *testing.T) {
	cluster := "👨‍👩‍👧‍👦"
	if got := FitGraphemeBytes(cluster+"!", len(cluster)); got != cluster {
		t.Fatalf("FitGraphemeBytes() = %q, want %q", got, cluster)
	}
	if got := FitGraphemeBytes(cluster+"!", 1); got != cluster {
		t.Fatalf("FitGraphemeBytes() split oversized first cluster: %q", got)
	}
}

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
