package cli

import (
	"strings"
	"testing"
)

// TestWrapIncrementalEquivalentToFull verifies that incremental re-wrapping
// (wrapIncremental) produces byte-identical output to a full re-wrap
// (wrapAllLines), across append, in-place rewrite, and truncation scenarios.
// This is the correctness invariant that lets TR-1 skip re-wrapping the clean
// prefix of the transcript.
func TestWrapIncrementalEquivalentToFull(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		widths []int
	}{
		{
			name:   "short-lines",
			lines:  []string{"hello", "world", "foo bar baz"},
			widths: []int{80, 40, 20, 10, 5},
		},
		{
			name:   "long-paragraphs",
			lines:  []string{strings.Repeat("the quick brown fox jumps over the lazy dog. ", 10)},
			widths: []int{80, 40, 20},
		},
		{
			name: "mixed-multi-line",
			lines: []string{
				"short",
				strings.Repeat("a b c d e f g h i j k l m n o p ", 8),
				"trailing single",
			},
			widths: []int{60, 30, 15},
		},
		{
			name:   "empty-and-blank",
			lines:  []string{"", "after blank", "", "tail"},
			widths: []int{80, 40},
		},
		{
			name:   "single-line",
			lines:  []string{"only one line"},
			widths: []int{80, 20, 5},
		},
	}

	for _, tc := range cases {
		for _, w := range tc.widths {
			t.Run(tc.name+"/"+itoa(w), func(t *testing.T) {
				// Reference: full wrap from scratch.
				var full []string
				for _, line := range tc.lines {
					full = append(full, strings.Split(wrapTranscript(line, w), "\n")...)
				}

				// Scenario A: build via wrapAllLines, then verify equality.
				mA := newTestChatTUI()
				mA.transcript = append([]string(nil), tc.lines...)
				mA.wrapAllLines(w)
				if got := strings.Join(mA.wrappedLines, "\n"); got != strings.Join(full, "\n") {
					t.Fatalf("wrapAllLines mismatch (width=%d):\ngot:  %q\nwant: %q", w, got, strings.Join(full, "\n"))
				}
				if len(mA.lineWrapCounts) != len(tc.lines) {
					t.Fatalf("lineWrapCounts len = %d, want %d", len(mA.lineWrapCounts), len(tc.lines))
				}
				// Verify per-line counts match a direct wrap.
				for i, line := range tc.lines {
					want := len(strings.Split(wrapTranscript(line, w), "\n"))
					if mA.lineWrapCounts[i] != want {
						t.Fatalf("lineWrapCounts[%d] = %d, want %d", i, mA.lineWrapCounts[i], want)
					}
				}

				// Scenario B: build incrementally — start empty, append one line
				// at a time, marking the tail dirty each step. Must equal full.
				mB := newTestChatTUI()
				mB.wrapDirtyFrom = -1
				for i, line := range tc.lines {
					mB.transcript = append(mB.transcript, line)
					if mB.wrapDirtyFrom < 0 {
						mB.wrapDirtyFrom = i
					}
					mB.wrapIncremental(w)
				}
				if got := strings.Join(mB.wrappedLines, "\n"); got != strings.Join(full, "\n") {
					t.Fatalf("incremental append mismatch (width=%d):\ngot:  %q\nwant: %q", w, got, strings.Join(full, "\n"))
				}

				// Scenario C: rewrite an in-place line (mark dirty from middle),
				// then incremental — must still equal a fresh full wrap.
				if len(tc.lines) >= 2 {
					mC := newTestChatTUI()
					mC.transcript = append([]string(nil), tc.lines...)
					mC.wrapAllLines(w)
					// Rewrite line 1 in place; mark dirty from 1.
					mC.transcript[1] = "REWRITTEN " + mC.transcript[1]
					mC.markTranscriptDirty(1)
					mC.wrapIncremental(w)
					// Reference: full wrap of the modified transcript.
					var wantFull []string
					for _, line := range mC.transcript {
						wantFull = append(wantFull, strings.Split(wrapTranscript(line, w), "\n")...)
					}
					if got := strings.Join(mC.wrappedLines, "\n"); got != strings.Join(wantFull, "\n") {
						t.Fatalf("in-place rewrite mismatch (width=%d):\ngot:  %q\nwant: %q", w, got, strings.Join(wantFull, "\n"))
					}
				}

				// Scenario D: width change triggers full re-wrap; result must
				// equal a direct full wrap at the new width.
				if len(tc.widths) > 1 {
					mD := newTestChatTUI()
					mD.transcript = append([]string(nil), tc.lines...)
					mD.wrapAllLines(w)
					// Simulate width change: wrapAllLines at new width.
					newW := w / 2
					if newW < 1 {
						newW = 1
					}
					mD.wrapAllLines(newW)
					var wantNew []string
					for _, line := range tc.lines {
						wantNew = append(wantNew, strings.Split(wrapTranscript(line, newW), "\n")...)
					}
					if got := strings.Join(mD.wrappedLines, "\n"); got != strings.Join(wantNew, "\n") {
						t.Fatalf("width-change re-wrap mismatch (newW=%d):\ngot:  %q\nwant: %q", newW, got, strings.Join(wantNew, "\n"))
					}
				}
			})
		}
	}
}

// TestWrapIncrementalTruncationFallback verifies that truncating the transcript
// (e.g. unsendPending pops lines) triggers a full re-wrap rather than a
// mismatched splice.
func TestWrapIncrementalTruncationFallback(t *testing.T) {
	lines := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	w := 40

	m := newTestChatTUI()
	m.transcript = append([]string(nil), lines...)
	m.wrapAllLines(w)

	// Reference full wrap of the truncated transcript.
	truncated := lines[:2]
	var want []string
	for _, line := range truncated {
		want = append(want, strings.Split(wrapTranscript(line, w), "\n")...)
	}

	// Truncate, mark dirty from 0, and incremental-wrap.
	m.transcript = truncated
	m.markTranscriptDirty(0)
	m.wrapIncremental(w)

	if got := strings.Join(m.wrappedLines, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("truncation mismatch:\ngot:  %q\nwant: %q", got, strings.Join(want, "\n"))
	}
	if len(m.lineWrapCounts) != len(truncated) {
		t.Fatalf("lineWrapCounts len after truncation = %d, want %d", len(m.lineWrapCounts), len(truncated))
	}
}

// TestMarkTranscriptDirtyExtendsRange verifies that markTranscriptDirty extends
// the dirty range downward (lower index) but never upward, so multiple dirty
// markers collapse to the minimum re-wrap prefix.
func TestMarkTranscriptDirtyExtendsRange(t *testing.T) {
	m := newTestChatTUI()
	m.wrapDirtyFrom = -1

	m.markTranscriptDirty(5)
	if m.wrapDirtyFrom != 5 {
		t.Fatalf("after mark(5): wrapDirtyFrom = %d, want 5", m.wrapDirtyFrom)
	}
	// A later dirty mark at a higher index must NOT shrink the range.
	m.markTranscriptDirty(8)
	if m.wrapDirtyFrom != 5 {
		t.Fatalf("after mark(8): wrapDirtyFrom = %d, want 5 (lower bound holds)", m.wrapDirtyFrom)
	}
	// An earlier dirty mark extends the range downward.
	m.markTranscriptDirty(2)
	if m.wrapDirtyFrom != 2 {
		t.Fatalf("after mark(2): wrapDirtyFrom = %d, want 2", m.wrapDirtyFrom)
	}
	// Negative index clamps to 0.
	m.wrapDirtyFrom = -1
	m.markTranscriptDirty(-3)
	if m.wrapDirtyFrom != 0 {
		t.Fatalf("after mark(-3): wrapDirtyFrom = %d, want 0", m.wrapDirtyFrom)
	}
}

// itoa avoids importing strconv just for one int-to-string in subtest names.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
