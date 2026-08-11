package fileutil

import "testing"

func TestGlobSetIncludeExcludeDoublestar(t *testing.T) {
	set, err := NewGlobSet([]string{"**/*.{go,ts}"}, []string{"**/vendor/**", "**/*.test.ts"})
	if err != nil {
		t.Fatalf("NewGlobSet: %v", err)
	}
	tests := map[string]bool{
		"main.go":                 true,
		"src/app.ts":              true,
		"src/app.test.ts":         false,
		"vendor/pkg/generated.go": false,
		"README.md":               false,
	}
	for path, want := range tests {
		if got := set.Match(path); got != want {
			t.Fatalf("Match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestGlobSetEmptyIncludeMeansAllExceptExcluded(t *testing.T) {
	set, err := NewGlobSet(nil, []string{"**/*.tmp"})
	if err != nil {
		t.Fatalf("NewGlobSet: %v", err)
	}
	if !set.Match("src/app.go") {
		t.Fatal("empty include should match ordinary file")
	}
	if set.Match("scratch.tmp") {
		t.Fatal("exclude pattern should remove tmp file")
	}
}

func TestMatchSlashGlobMatchesRootWithRecursivePrefix(t *testing.T) {
	if !MatchSlashGlob("main.go", "**/*.go") {
		t.Fatal("**/*.go should match root-level main.go")
	}
}
