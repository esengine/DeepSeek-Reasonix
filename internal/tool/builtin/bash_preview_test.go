package builtin

import "testing"

// TestCommandPreviewCollapsesAllASCIIWhitespace pins the new behavior:
// every ASCII whitespace run collapses to a single ASCII space, with no
// stray \t or \r leaking into the status-bar row. The previous shape only
// replaced \n, so a script with a tab between commands rendered as
// "make\tbuild" (and the terminal's tab-stop dance made the row jump).
func TestCommandPreviewCollapsesAllASCIIWhitespace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "make build", "make build"},
		{"newline", "make\nbuild", "make build"},
		{"tab", "make\tbuild", "make build"},
		{"cr", "make\rbuild", "make build"},
		{"crlf", "make\r\nbuild", "make build"},
		{"vt", "make\vbuild", "make build"},
		{"ff", "make\fbuild", "make build"},
		{"mixed", "make\t\n build", "make build"},
		{"leading", "\n  make build", "make build"},
		{"trailing", "make build  \t\n", "make build"},
		{"run", "  npm  run  dev  ", "npm run dev"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandPreview(tc.in); got != tc.want {
				t.Errorf("commandPreview(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCommandPreviewTruncatesWithEllipsis pins the 48-rune truncation
// contract that the status bar relies on for its "…" overflow.
func TestCommandPreviewTruncatesWithEllipsis(t *testing.T) {
	// 60 runes of payload + 1 space at the start should land at 48 runes
	// + the ellipsis (U+2026).
	in := "echo " + stringRepeat("x", 60)
	got := commandPreview(in)
	wantRunes := 48
	if gotRunes := len([]rune(got)); gotRunes != wantRunes+1 { // 48 + "…"
		t.Errorf("rune count = %d, want %d", gotRunes, wantRunes+1)
	}
	if !endsWithEllipsis(got) {
		t.Errorf("truncated output should end with U+2026: %q", got)
	}
}

// TestCommandPreviewPreservesNonASCII: non-ASCII characters (CJK, accented
// Latin) should pass through unchanged; only the run-collapsing behavior
// changes. This protects a regression where a future refactor reaches for
// the unicode whitespace table and starts treating U+00A0 as a separator.
func TestCommandPreviewPreservesNonASCII(t *testing.T) {
	in := "echo 你好 café"
	if got := commandPreview(in); got != in {
		t.Errorf("commandPreview(%q) = %q, want %q", in, got, in)
	}
}

func stringRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func endsWithEllipsis(s string) bool {
	r := []rune(s)
	if len(r) == 0 {
		return false
	}
	return r[len(r)-1] == '…'
}
