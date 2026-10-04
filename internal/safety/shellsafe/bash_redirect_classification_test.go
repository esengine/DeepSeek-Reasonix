package shellsafe

import (
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestBashRedirectsToNamedFile(t *testing.T) {
	for _, tt := range []struct {
		source  string
		writes  bool
		decided bool
	}{
		{"", false, true},
		{"echo fixture", false, true},
		{"echo '", false, false},
		{"echo fixture >", false, false},
		{"echo fixture > output", true, true},
		{"echo fixture >> output", true, true},
		{"echo fixture >| output", true, true},
		{"echo fixture &> output", true, true},
		{"echo fixture &>> output", true, true},
		{"echo fixture >$target", true, true},
		{"echo fixture >/dev/null", false, true},
		{"echo fixture >NUL", false, true},
		{"echo fixture >$null", false, true},
		{"echo fixture 2>&1", false, true},
		{"cat <input", false, true},
		{"echo fixture >/dev/null; echo fixture >output", true, true},
		{"echo fixture >output | cat", true, true},
		{"echo fixture | cat >output", true, true},
		{"echo fixture && echo fixture >output", true, true},
		{"echo fixture || echo fixture >/dev/null", false, true},
	} {
		t.Run(tt.source, func(t *testing.T) {
			writes, decided := BashRedirectsToNamedFile(tt.source)
			if writes != tt.writes || decided != tt.decided {
				t.Fatalf("got (%v, %v), want (%v, %v)", writes, decided, tt.writes, tt.decided)
			}
		})
	}
}

func TestRedirectNormalizationBoundaries(t *testing.T) {
	for _, tt := range []struct {
		source string
		want   string
		ok     bool
	}{
		{"", "", true},
		{"echo '", "", false},
		{"echo fixture >", "", false},
		{"echo fixture 2>&file", "", false},
		{"echo fixture 2>&$fd", "", false},
		{"echo fixture >/dev/null | cat 2>&1", "echo fixture  | cat", true},
		{"echo fixture >/dev/null && cat <input", "", false},
		{"echo fixture >/dev/null | cat >output", "", false},
		{"echo fixture >|/dev/null", "echo fixture", true},
	} {
		t.Run(tt.source, func(t *testing.T) {
			got, ok := NormalizeBashSafeRedirectsForMatch(tt.source)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRedirectMalformedTreeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		stmt    *syntax.Stmt
		decided bool
	}{
		{"nil statement", nil, true},
		{"nil redirect", &syntax.Stmt{Redirs: []*syntax.Redirect{nil}}, false},
		{"malformed left branch", &syntax.Stmt{Cmd: &syntax.BinaryCmd{X: &syntax.Stmt{Redirs: []*syntax.Redirect{nil}}}}, false},
		{"malformed right branch", &syntax.Stmt{Cmd: &syntax.BinaryCmd{Y: &syntax.Stmt{Redirs: []*syntax.Redirect{nil}}}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writes, decided := stmtRedirectsToNamedFile("", tt.stmt)
			if writes || decided != tt.decided {
				t.Fatalf("got (%v, %v), want (false, %v)", writes, decided, tt.decided)
			}
			var spans []redirectSpan
			if ok := appendSafeRedirectSpans("", tt.stmt, &spans); ok != tt.decided {
				t.Fatalf("normalization allowed = %v, want %v", ok, tt.decided)
			}
		})
	}
	for _, word := range []*syntax.Word{nil, {Parts: []syntax.WordPart{&syntax.Lit{Value: "1"}}}, {Parts: []syntax.WordPart{&syntax.Lit{ValuePos: syntax.NewPos(10, 1, 11), Value: "1"}}}} {
		if got := redirectWordSource("", word); got != "" {
			t.Fatalf("got %q, want empty source for invalid word", got)
		}
		if isSafeFDDupWord("", word) {
			t.Fatal("invalid word accepted as a file descriptor")
		}
	}
}
