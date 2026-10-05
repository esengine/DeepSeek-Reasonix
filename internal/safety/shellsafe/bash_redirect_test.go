package shellsafe

import "testing"

// Every spelling NormalizeBashSafeRedirectsForMatch documents as safe must
// strip to the bare command: what survives here is what prefix and read-only
// matching then judge, so a form that fails to strip silently makes a
// read-only command look like a write.
func TestSafeRedirectsStripToTheBareCommand(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"git log 2>&1", "git log"},
		{"git log >&2", "git log"},
		{"git log 2>&-", "git log"},
		{"git log 0<&1", "git log"},
		{"git log >/dev/null", "git log"},
		{"git log 2>/dev/null", "git log"},
		{"git log 2> /dev/null", "git log"},
		{"git log >>/dev/null", "git log"},
		{"git log &>/dev/null", "git log"},
		{"git log &>>/dev/null", "git log"},
		{"git log >$null", "git log"},
		{"git log >nul", "git log"},
		{"git log >NUL", "git log"},
		{"git log 2>/dev/null 1>&2", "git log"},
		{"git status; git log 2>/dev/null", "git status; git log"},
	} {
		got, ok := NormalizeBashSafeRedirectsForMatch(tt.in)
		if !ok {
			t.Errorf("NormalizeBashSafeRedirectsForMatch(%q) refused a documented safe form", tt.in)
			continue
		}
		if trimmed(got) != tt.want {
			t.Errorf("NormalizeBashSafeRedirectsForMatch(%q) = %q, want %q", tt.in, trimmed(got), tt.want)
		}
	}
}

// A redirect that can reach a real file must not be stripped, in any statement
// of the line: stripping one would hand the matcher a command that no longer
// writes.
func TestFileReachingRedirectsRefuseNormalization(t *testing.T) {
	for _, in := range []string{
		"git log > out.txt",
		"git log >> out.txt",
		"git log 2> err.log",
		"git log &> all.log",
		"git log < input.txt",
		"git status; git log > out.txt",
		"git log > out.txt; git status",
		"git log >/dev/null; git diff > out.txt",
		"git log > /dev/nullish",
	} {
		if got, ok := NormalizeBashSafeRedirectsForMatch(in); ok {
			t.Errorf("NormalizeBashSafeRedirectsForMatch(%q) = %q, true; a real file must refuse", in, got)
		}
	}
}

func TestNoRedirectPassesThroughUnchanged(t *testing.T) {
	const in = "git log --oneline -5"
	got, ok := NormalizeBashSafeRedirectsForMatch(in)
	if !ok || got != in {
		t.Fatalf("NormalizeBashSafeRedirectsForMatch(%q) = %q, %v", in, got, ok)
	}
}

// A heredoc carries its body past the parser's statement span, so the subject
// cannot be rewritten by cutting spans out of it.
func TestHeredocRefusesNormalization(t *testing.T) {
	if _, ok := NormalizeBashSafeRedirectsForMatch("cat <<EOF\nbody\nEOF"); ok {
		t.Fatal("a heredoc must refuse normalization")
	}
}

func trimmed(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
