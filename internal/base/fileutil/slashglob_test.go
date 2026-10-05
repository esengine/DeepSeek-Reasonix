package fileutil

import "testing"

func TestMatchSlashGlobMatchesRootWithRecursivePrefix(t *testing.T) {
	if !MatchSlashGlob("main.go", "**/*.go") {
		t.Fatal("**/*.go should match root-level main.go")
	}
	if !MatchSlashGlob("src/app.ts", "**/*.{go,ts}") {
		t.Fatal("a nested file should match its own recursive pattern")
	}
	if MatchSlashGlob("README.md", "**/*.{go,ts}") {
		t.Fatal("a pattern that names neither extension must not match")
	}
}

func TestNormalizeSlashPathSettlesSeparatorAndDots(t *testing.T) {
	if got := NormalizeSlashPath("src/../src/app.go"); got != "src/app.go" {
		t.Fatalf("NormalizeSlashPath = %q, want src/app.go", got)
	}
}
