package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLiteralEditKeepsTheLineEndingOfTheLineItLandsIn(t *testing.T) {
	for _, tc := range []struct {
		name, content, old, new, want string
		replaceAll                    bool
	}{
		{"CRLF file, single-line old", "[client]\r\nretries=3\r\nbackoff=2\r\n", "retries=3", "retries=3\ntimeout=30", "[client]\r\nretries=3\r\ntimeout=30\r\nbackoff=2\r\n", false},
		{"CRLF file, replace_all", "a=1\r\nb=1\r\n", "=1", "=1\n# x", "a=1\r\n# x\r\nb=1\r\n# x\r\n", true},
		{"CRLF file, no trailing newline", "a\r\nretries=3", "retries=3", "retries=3\nx", "a\r\nretries=3\r\nx", false},
		{"LF file stays LF", "[client]\nretries=3\n", "retries=3", "retries=3\ntimeout=30", "[client]\nretries=3\ntimeout=30\n", false},
		{"mixed file, LF region stays LF", "head\r\nretries=3\nbackoff=2\n", "retries=3", "retries=3\ntimeout=30", "head\r\nretries=3\ntimeout=30\nbackoff=2\n", false},
		{"mixed file, CRLF region gets CRLF", "retries=3\r\nbackoff=2\nx\n", "retries=3", "retries=3\ntimeout=30", "retries=3\r\ntimeout=30\r\nbackoff=2\nx\n", false},
		{"mixed file, replace_all follows each match", "a=1\r\nb=1\n", "=1", "=1\n#", "a=1\r\n#\r\nb=1\n#\n", true},
		{"replacement that already uses CRLF is not doubled", "a\r\nb\r\n", "a", "a\r\nz", "a\r\nz\r\nb\r\n", false},
		{"no line break in the replacement", "a\r\nb\r\n", "a", "c", "c\r\nb\r\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := applyOldStringEdit(tc.content, tc.old, tc.new, tc.replaceAll)
			if got.updated != tc.want {
				t.Fatalf("updated = %q, want %q", got.updated, tc.want)
			}
			if strings.Contains(got.receipt.replacement, "\n") && !strings.Contains(tc.want, "\n") {
				t.Fatalf("receipt replacement %q does not describe what was written", got.receipt.replacement)
			}
		})
	}
}

func TestEditFileLiteralMatchInCRLFFileLeavesNoBareLF(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "settings.ini"), []byte("[client]\r\nretries=3\r\nbackoff=2\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runEdit(t, dir, "settings.ini", "retries=3", "retries=3\ntimeout=30")
	raw, err := os.ReadFile(filepath.Join(dir, "settings.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), "[client]\r\nretries=3\r\ntimeout=30\r\nbackoff=2\r\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}
