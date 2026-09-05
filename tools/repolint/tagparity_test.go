package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// The tree's two copies agree today. This is the check's own regression: both
// declarations are matched by shape, so moving or renaming either one fails
// here rather than silently passing with an empty list.
func TestTransientTagParityIsCleanInTree(t *testing.T) {
	if got := checkTransientTagParity("../.."); len(got) != 0 {
		t.Fatalf("checkTransientTagParity = %v, want none", got)
	}
	kernel, err := tagsIn("../..", goTagFile, goTagListRe, goTagRe)
	if err != nil {
		t.Fatal(err)
	}
	// The tag that motivated the check: its absence is what put host markup in
	// the transcript, and an empty parse would report parity just as happily.
	if !slices.Contains(kernel, "background-jobs") {
		t.Fatalf("kernel tags = %v, want background-jobs among them", kernel)
	}
}

// A page that knows fewer tags than the kernel prepends is the failure this
// exists for, and it has to be named per missing tag — a count would not say
// which block is about to be printed at the reader.
func TestTransientTagParityReportsBothDirections(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(goTagFile, "package agent\n\nvar TransientUserBlockTags = []string{\n\t\"kept\",\n\t\"only-kernel\",\n}\n")
	write(tsTagFile, "const CONTROL =\n  /<(kept|only-page)[\\s\\S]*?<\\/\\1>\\s*/g;\n")

	got := checkTransientTagParity(root)
	if len(got) != 2 {
		t.Fatalf("checkTransientTagParity = %v, want one finding each way", got)
	}
	missing := regexp.MustCompile(`<only-kernel> is prepended`)
	stale := regexp.MustCompile(`page strips <only-page>`)
	if !missing.MatchString(got[0].Msg) || got[0].File != tsTagFile {
		t.Errorf("first finding = %+v, want the page missing only-kernel", got[0])
	}
	if !stale.MatchString(got[1].Msg) || got[1].File != goTagFile {
		t.Errorf("second finding = %+v, want the kernel missing only-page", got[1])
	}
}

// A declaration the check can no longer find must fail loudly. Returning an
// empty set would read as parity and put the drift back where it started.
func TestTransientTagParityFailsWhenDeclarationMoves(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, goTagFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkTransientTagParity(root)
	if len(got) != 1 || got[0].File != goTagFile {
		t.Fatalf("checkTransientTagParity = %v, want one finding against the kernel file", got)
	}
}
