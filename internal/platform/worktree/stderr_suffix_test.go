package worktree

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStderrSuffixKeepsTruncatedStderrValidUTF8(t *testing.T) {
	// git can print localized (multi-byte) messages; 499 ASCII bytes push the
	// next three-byte rune across the 500-byte cap.
	stderr := strings.Repeat("a", 499) + "致命错误：无法创建工作区"
	got := stderrSuffix(stderr)
	if !utf8.ValidString(got) {
		t.Fatalf("stderrSuffix produced invalid UTF-8: %q", got[len(got)-8:])
	}
	if want := ": " + strings.Repeat("a", 499) + "…"; got != want {
		t.Fatalf("stderrSuffix = %q, want %q", got[len(got)-8:], want[len(want)-8:])
	}

	short := "fatal: 无效的引用"
	if got := stderrSuffix(short); got != ": "+short {
		t.Fatalf("stderrSuffix(%q) = %q, want it unchanged", short, got)
	}
}
