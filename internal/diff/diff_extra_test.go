package diff

import (
	"strings"
	"testing"
)

// --- splitLines ---

func TestSplitLinesEmpty(t *testing.T) {
	lines, eol := splitLines("")
	if len(lines) != 0 {
		t.Errorf("empty: got %d lines", len(lines))
	}
	if !eol {
		t.Error("empty should report endsWithNewline=true")
	}
}

func TestSplitLinesNoTrailingNewline(t *testing.T) {
	lines, eol := splitLines("a\nb")
	if len(lines) != 2 {
		t.Errorf("got %d lines", len(lines))
	}
	if lines[0] != "a" || lines[1] != "b" {
		t.Errorf("lines = %v", lines)
	}
	if eol {
		t.Error("should report no trailing newline")
	}
}

func TestSplitLinesTrailingNewline(t *testing.T) {
	lines, eol := splitLines("a\nb\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines", len(lines))
	}
	if !eol {
		t.Error("should report trailing newline")
	}
}

func TestSplitLinesSingleLine(t *testing.T) {
	lines, eol := splitLines("hello")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("lines = %v", lines)
	}
	if eol {
		t.Error("single line without newline should report false")
	}
}

func TestSplitLinesSingleLineWithNewline(t *testing.T) {
	lines, eol := splitLines("hello\n")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("lines = %v", lines)
	}
	if !eol {
		t.Error("single line with newline should report true")
	}
}

// --- isBinary ---

func TestIsBinaryEmpty(t *testing.T) {
	if isBinary("") {
		t.Error("empty string is not binary")
	}
}

func TestIsBinaryText(t *testing.T) {
	if isBinary("hello world\nline 2") {
		t.Error("plain text is not binary")
	}
}

func TestIsBinaryNUL(t *testing.T) {
	if !isBinary("hello\x00world") {
		t.Error("string with NUL should be binary")
	}
}

func TestIsBinaryNULAtEnd(t *testing.T) {
	if !isBinary("hello\x00") {
		t.Error("NUL at end should be binary")
	}
}

// --- Build edge cases ---

func TestBuildBothEmpty(t *testing.T) {
	c := Build("empty.txt", "", "", Modify)
	if c.Diff != "" || c.Added != 0 || c.Removed != 0 {
		t.Errorf("both empty should be no-op: %+v", c)
	}
}

func TestBuildEmptyOldNonEmptyNew(t *testing.T) {
	c := Build("new.txt", "", "line1\nline2\n", Create)
	if c.Added != 2 || c.Removed != 0 {
		t.Errorf("added/removed = %d/%d", c.Added, c.Removed)
	}
	if c.Kind != Create {
		t.Errorf("kind = %q", c.Kind)
	}
}

func TestBuildNonEmptyOldEmptyNew(t *testing.T) {
	c := Build("del.txt", "line1\nline2\n", "", Delete)
	if c.Added != 0 || c.Removed != 2 {
		t.Errorf("added/removed = %d/%d", c.Added, c.Removed)
	}
}

func TestBuildCRLF(t *testing.T) {
	old := "line1\r\nline2\r\n"
	neu := "line1\r\nLINE2\r\n"
	c := Build("crlf.txt", old, neu, Modify)
	// CRLF should be handled (split on \n, \r stays in line content).
	if c.Added != 1 || c.Removed != 1 {
		t.Errorf("added/removed = %d/%d, want 1/1", c.Added, c.Removed)
	}
}

func TestBuildWhitespaceOnly(t *testing.T) {
	old := "a\tb\n"
	neu := "a  b\n"
	c := Build("ws.txt", old, neu, Modify)
	if c.Added != 1 || c.Removed != 1 {
		t.Errorf("added/removed = %d/%d", c.Added, c.Removed)
	}
}

func TestBuildLargeFile(t *testing.T) {
	// Build a large file with one changed line in the middle.
	var oldB, newB strings.Builder
	for i := 0; i < 1000; i++ {
		oldB.WriteString("line\n")
		if i == 500 {
			newB.WriteString("CHANGED\n")
		} else {
			newB.WriteString("line\n")
		}
	}
	c := Build("large.txt", oldB.String(), newB.String(), Modify)
	if c.Added != 1 || c.Removed != 1 {
		t.Errorf("added/removed = %d/%d, want 1/1", c.Added, c.Removed)
	}
}

// --- Kind constants ---

func TestKindConstants(t *testing.T) {
	if Create != "create" {
		t.Errorf("Create = %q", Create)
	}
	if Modify != "modify" {
		t.Errorf("Modify = %q", Modify)
	}
	if Delete != "delete" {
		t.Errorf("Delete = %q", Delete)
	}
}

// --- itoa ---

func TestItoa(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1000, "1000"},
	}
	for _, c := range cases {
		if got := itoa(c.n); got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// --- rangeSpec ---

func TestRangeSpec(t *testing.T) {
	if got := rangeSpec(5, 1); got != "5" {
		t.Errorf("rangeSpec(5,1) = %q", got)
	}
	if got := rangeSpec(5, 3); got != "5,3" {
		t.Errorf("rangeSpec(5,3) = %q", got)
	}
}

// --- lastLineNumbers ---

func TestLastLineNumbers(t *testing.T) {
	refs := []lineRef{
		{op: op{opEqual, "a"}, oldNo: 1, newNo: 1},
		{op: op{opDelete, "b"}, oldNo: 2},
		{op: op{opInsert, "c"}, newNo: 2},
		{op: op{opEqual, "d"}, oldNo: 3, newNo: 3},
	}
	lastOld, lastNew := lastLineNumbers(refs)
	if lastOld != 3 || lastNew != 3 {
		t.Errorf("lastOld=%d, lastNew=%d", lastOld, lastNew)
	}
}

// --- group ---

func TestGroupNoChanges(t *testing.T) {
	refs := []lineRef{
		{op: op{opEqual, "a"}, oldNo: 1, newNo: 1},
	}
	hunks := group(refs, 3)
	if len(hunks) != 0 {
		t.Errorf("no changes should yield no hunks, got %d", len(hunks))
	}
}

func TestGroupMergeAdjacent(t *testing.T) {
	// Two changes close together should merge into one hunk.
	refs := make([]lineRef, 10)
	for i := range refs {
		refs[i] = lineRef{op: op{opEqual, "x"}, oldNo: i + 1, newNo: i + 1}
	}
	refs[3] = lineRef{op: op{opDelete, "y"}, oldNo: 4}
	refs[4] = lineRef{op: op{opInsert, "z"}, newNo: 4}
	refs[6] = lineRef{op: op{opDelete, "w"}, oldNo: 7}
	refs[7] = lineRef{op: op{opInsert, "v"}, newNo: 7}
	hunks := group(refs, 3)
	if len(hunks) != 1 {
		t.Errorf("adjacent changes should merge, got %d hunks", len(hunks))
	}
}
