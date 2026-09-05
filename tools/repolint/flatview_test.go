package main

import "testing"

const flatViewSource = `package p

import "reasonix/internal/frontmatter"

func a(s string) map[string]string {
	fm, _ := frontmatter.SplitLegacy(s)
	return fm
}

func b(s string) map[string]string {
	doc, _ := frontmatter.Parse(s)
	return doc.LegacyFlat()
}

func c(s string) bool {
	doc, _ := frontmatter.Parse(s)
	return doc.Has("delivery", "review-report")
}
`

func TestFlatViewFlagsBothEntryPointsOutsideTheList(t *testing.T) {
	got := checkFlatView(parseBytes("internal/elsewhere/x.go", []byte(flatViewSource)))
	if len(got) != 2 {
		t.Fatalf("findings = %d, want one per entry point: %v", len(got), got)
	}
	for _, f := range got {
		if f.Rule != ruleFlatView || f.Weight != 1 {
			t.Fatalf("a pass/fail rule must weigh one: %+v", f)
		}
	}
}

// Reading the structure is the whole point of the boundary, so a declared
// reader and the structured accessors are both left alone.
func TestFlatViewLeavesDeclaredReadersAndStructuredAccessAlone(t *testing.T) {
	if got := checkFlatView(parseBytes("internal/skill/skill.go", []byte(flatViewSource))); len(got) != 0 {
		t.Fatalf("a declared reader was flagged: %v", got)
	}
	structured := `package p

import "reasonix/internal/frontmatter"

func c(s string) bool {
	doc, _ := frontmatter.Parse(s)
	if _, ok := doc.Lookup("delivery", "review-report"); ok {
		return true
	}
	return doc.Has("authority")
}
`
	if got := checkFlatView(parseBytes("internal/elsewhere/x.go", []byte(structured))); len(got) != 0 {
		t.Fatalf("structured access was flagged: %v", got)
	}
}

// The list is a promise about real files. A stale entry would quietly exempt a
// path that no longer exists, and the boundary would loosen without a diff.
func TestEveryDeclaredFlatViewReaderStillReadsIt(t *testing.T) {
	for _, rel := range flatViewReaders {
		src, err := parseSource("../..", rel)
		if err != nil || src == nil || src.file == nil {
			t.Errorf("declared reader %q does not parse; drop it from the list", rel)
			continue
		}
		if len(checkFlatViewIgnoringList(src)) == 0 {
			t.Errorf("declared reader %q no longer reads the flat view; drop it from the list so the boundary tightens", rel)
		}
	}
}
