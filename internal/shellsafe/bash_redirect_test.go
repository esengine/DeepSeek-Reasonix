package shellsafe

import "testing"

// TestNormalizeBashSafeRedirectsForMatch covers the fd dup/close forms
// (2>&1, >&2, 2>&-, 0<&1, N>&M), null-sink output redirects (>/dev/null,
// >$null, >nul, &>>), and the fail-closed rejections (heredocs, malformed
// input, and any redirect that can touch a real file).
func TestNormalizeBashSafeRedirectsForMatch(t *testing.T) {
	okCases := []struct {
		in   string
		want string
	}{
		// fd duplication and close.
		{"git status 2>&1", "git status"},
		{"ls >&2", "ls"},
		{"cmd 2>&-", "cmd"},
		{"cmd 0<&1", "cmd"},
		{"cmd 3>&1", "cmd"},
		// null sinks: /dev/null, $null, nul (case-insensitive), all ops.
		{"ls >/dev/null", "ls"},
		{"ls 2>/dev/null", "ls"},
		{"ls >>/dev/null", "ls"},
		{"ls &>/dev/null", "ls"},
		{"ls &>>/dev/null", "ls"},
		{"ls > /dev/null", "ls"},
		{"cmd 2> /dev/null", "cmd"},
		{"Get-ChildItem > $null", "Get-ChildItem"},
		{"cmd > nul", "cmd"},
		{"cmd >nul", "cmd"},
		{"cmd >$null", "cmd"},
		{"cmd >NUL", "cmd"},
		// multiple safe redirects (span removal leaves surrounding whitespace).
		{"ls -la >/dev/null 2>&1", "ls -la"},
		{"ls 2>&1 -la", "ls  -la"},
		// no redirects at all: unchanged.
		{"git status", "git status"},
		// safe redirects inside a chain are stripped without touching the chain.
		{"git status && ls >/dev/null", "git status && ls"},
	}
	for _, tt := range okCases {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := NormalizeBashSafeRedirectsForMatch(tt.in)
			if !ok {
				t.Fatalf("NormalizeBashSafeRedirectsForMatch(%q) = (\"\", false), want (%q, true)", tt.in, tt.want)
			}
			if got != tt.want {
				t.Errorf("NormalizeBashSafeRedirectsForMatch(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	rejectCases := []string{
		// redirects that can write a real file.
		"ls > out.txt",
		"ls 2> err.txt",
		"ls >> log.txt",
		"ls < in.txt",
		"ls >| clobber.txt",
		"ls <>file",
		// one safe + one unsafe redirect.
		"ls >/dev/null > out.txt",
		"ls 2>&1 > out.txt",
		// fd dup to a non-numeric target.
		"cmd 2>&x",
		// malformed / incomplete redirects.
		"ls >",
		"cmd 2>&",
		// heredocs are always rejected.
		"cat <<EOF\nhello\nEOF",
		// parse failures.
		"echo \"unterminated",
	}
	for _, in := range rejectCases {
		t.Run(in, func(t *testing.T) {
			got, ok := NormalizeBashSafeRedirectsForMatch(in)
			if ok {
				t.Errorf("NormalizeBashSafeRedirectsForMatch(%q) = (%q, true), want fail-closed (\"\", false)", in, got)
			}
		})
	}
}
