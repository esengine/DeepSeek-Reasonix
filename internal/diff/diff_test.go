package diff

import (
	"strings"
	"testing"
)

func TestBuild_NoChange(t *testing.T) {
	c := Build("f.txt", "a\nb\n", "a\nb\n", Modify)
	if c.Diff != "" || c.Added != 0 || c.Removed != 0 {
		t.Fatalf("identical content should yield empty diff, got %+v", c)
	}
}

func TestBuild_Create(t *testing.T) {
	c := Build("new.txt", "", "hello\nworld\n", Create)
	if c.Added != 2 || c.Removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 2/0", c.Added, c.Removed)
	}
	if !strings.Contains(c.Diff, "@@ -0,0 +1,2 @@") {
		t.Fatalf("create hunk header missing:\n%s", c.Diff)
	}
	if !strings.Contains(c.Diff, "+hello") || !strings.Contains(c.Diff, "+world") {
		t.Fatalf("added lines missing:\n%s", c.Diff)
	}
}

func TestBuild_DeleteAll(t *testing.T) {
	c := Build("gone.txt", "x\ny\n", "", Delete)
	if c.Added != 0 || c.Removed != 2 {
		t.Fatalf("added/removed = %d/%d, want 0/2", c.Added, c.Removed)
	}
	if !strings.Contains(c.Diff, "@@ -1,2 +0,0 @@") {
		t.Fatalf("delete hunk header missing:\n%s", c.Diff)
	}
}

func TestBuild_ModifyMiddle(t *testing.T) {
	old := "1\n2\n3\n4\n5\n"
	neu := "1\n2\nThree\n4\n5\n"
	c := Build("m.txt", old, neu, Modify)
	if c.Added != 1 || c.Removed != 1 {
		t.Fatalf("added/removed = %d/%d, want 1/1", c.Added, c.Removed)
	}
	if !strings.Contains(c.Diff, "-3") || !strings.Contains(c.Diff, "+Three") {
		t.Fatalf("expected -3/+Three:\n%s", c.Diff)
	}
	// 3 lines of context each side, but the file only has 2 before and 2 after.
	if !strings.Contains(c.Diff, " 1") || !strings.Contains(c.Diff, " 5") {
		t.Fatalf("context lines missing:\n%s", c.Diff)
	}
}

func TestBuild_Prepend(t *testing.T) {
	c := Build("p.txt", "b\nc\n", "a\nb\nc\n", Modify)
	if c.Added != 1 || c.Removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 1/0", c.Added, c.Removed)
	}
	if !strings.Contains(c.Diff, "@@ -1,2 +1,3 @@") {
		t.Fatalf("prepend header wrong:\n%s", c.Diff)
	}
}

func TestBuild_TwoSeparateHunks(t *testing.T) {
	// Two changes far enough apart (>2*context) to form distinct hunks.
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	neu := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nK\nl\n"
	c := Build("two.txt", old, neu, Modify)
	if got := strings.Count(c.Diff, "@@ "); got != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", got, c.Diff)
	}
	if c.Added != 2 || c.Removed != 2 {
		t.Fatalf("added/removed = %d/%d, want 2/2", c.Added, c.Removed)
	}
}

func TestBuild_AdjacentChangesMerge(t *testing.T) {
	// Changes within 2*context collapse into one hunk.
	old := "a\nb\nc\nd\ne\n"
	neu := "a\nB\nc\nD\ne\n"
	c := Build("adj.txt", old, neu, Modify)
	if got := strings.Count(c.Diff, "@@ "); got != 1 {
		t.Fatalf("expected 1 merged hunk, got %d:\n%s", got, c.Diff)
	}
}

func TestBuild_NoNewlineAtEOF(t *testing.T) {
	c := Build("nonl.txt", "a\nb", "a\nc", Modify)
	if !strings.Contains(c.Diff, "\\ No newline at end of file") {
		t.Fatalf("expected no-newline marker:\n%s", c.Diff)
	}
}

func TestBuild_Binary(t *testing.T) {
	c := Build("bin", "ok\n", "bad\x00data", Modify)
	if !c.Binary {
		t.Fatal("expected Binary=true")
	}
	if c.Diff != "" || c.Added != 0 || c.Removed != 0 {
		t.Fatalf("binary change should carry no textual diff, got %+v", c)
	}
}

// TestMyers_MinimalEditScript checks the diff is the shortest edit script: a
// single line inserted into a run of identical lines must not be rendered as a
// block delete+insert.
func TestMyers_MinimalEditScript(t *testing.T) {
	old := "x\nx\nx\n"
	neu := "x\nx\ny\nx\n"
	c := Build("min.txt", old, neu, Modify)
	if c.Added != 1 || c.Removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 1/0 (minimal):\n%s", c.Added, c.Removed, c.Diff)
	}
}

// TestApplyOpsReconstruct verifies the op stream is internally consistent: the
// equal+delete lines reproduce the old file and equal+insert lines reproduce
// the new file, for a spread of cases.
func TestApplyOpsReconstruct(t *testing.T) {
	cases := [][2]string{
		{"", "abc\n"},
		{"abc\n", ""},
		{"a\nb\nc\n", "a\nc\n"},
		{"a\nc\n", "a\nb\nc\n"},
		{"one\ntwo\nthree\n", "ONE\ntwo\nTHREE\nfour\n"},
		{"same\nsame\n", "same\nsame\n"},
	}
	for _, tc := range cases {
		oldLines, _ := splitLines(tc[0])
		newLines, _ := splitLines(tc[1])
		ops, _ := myers(oldLines, newLines)
		var gotOld, gotNew []string
		for _, o := range ops {
			switch o.typ {
			case opEqual:
				gotOld = append(gotOld, o.line)
				gotNew = append(gotNew, o.line)
			case opDelete:
				gotOld = append(gotOld, o.line)
			case opInsert:
				gotNew = append(gotNew, o.line)
			}
		}
		if strings.Join(gotOld, "\n") != strings.Join(oldLines, "\n") {
			t.Errorf("old reconstruction mismatch for %q→%q", tc[0], tc[1])
		}
		if strings.Join(gotNew, "\n") != strings.Join(newLines, "\n") {
			t.Errorf("new reconstruction mismatch for %q→%q", tc[0], tc[1])
		}
	}
}
