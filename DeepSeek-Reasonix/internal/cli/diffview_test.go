package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestDiffBodyDropsHeadersKeepsLineNumbers(t *testing.T) {
	d := event.FileDiff{Diff: "--- a/x.go\n+++ b/x.go\n@@ -7 +7 @@\n-old\n+new\n", Added: 1, Removed: 1}
	joined := strings.Join(diffBody(d, "x.go", 80, 40), "\n")
	if strings.Contains(joined, "--- a/") || strings.Contains(joined, "+++ b/") || strings.Contains(joined, "@@") {
		t.Fatalf("file/hunk headers should be dropped, got:\n%s", joined)
	}
	for _, want := range []string{"old", "new", "7"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q (code or line number) in:\n%s", want, joined)
		}
	}
}

func TestDiffBodyFolds(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- a/x\n+++ b/x\n@@ -1,8 +1,8 @@\n")
	for i := 0; i < 8; i++ {
		b.WriteString("+line\n")
	}
	body := diffBody(event.FileDiff{Diff: b.String()}, "x", 80, 5)
	if len(body) != 5 {
		t.Fatalf("want 5 rows (4 content + footer), got %d:\n%s", len(body), strings.Join(body, "\n"))
	}
	// 8 rendered add rows minus the 4 kept = 4 folded.
	if !strings.Contains(body[len(body)-1], "4") {
		t.Fatalf("footer should report 4 folded lines, got %q", body[len(body)-1])
	}
}

func TestDiffBodyNoFoldWhenShort(t *testing.T) {
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n+a\n"}
	if got := len(diffBody(d, "x", 80, 40)); got != 1 {
		t.Fatalf("want 1 unfolded row, got %d", got)
	}
}

func TestDiffBlockHeader(t *testing.T) {
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n-a\n+b\n", Added: 1, Removed: 1}
	block := diffBlock("edit_file", `{"path":"pkg/x.go"}`, d, 80, 40)
	if len(block) == 0 || !strings.Contains(block[0], "Update") || !strings.Contains(block[0], "pkg/x.go") {
		t.Fatalf("header should name verb + path, got %q", block[0])
	}
}

func TestDiffBlockNilWithoutDiff(t *testing.T) {
	if diffBlock("write_file", `{"path":"x"}`, event.FileDiff{}, 80, 40) != nil {
		t.Fatal("no diff should yield no block")
	}
}

func TestDiffPath(t *testing.T) {
	if got := diffPath(`{"path":"a/b.go","old_string":"x"}`); got != "a/b.go" {
		t.Fatalf("got %q", got)
	}
	if got := diffPath(`not json`); got != "" {
		t.Fatalf("malformed args should yield empty path, got %q", got)
	}
}

func TestDiffBarReappliesBackground(t *testing.T) {
	defer func(prev bool) { colorEnabled = prev }(colorEnabled)
	colorEnabled = true

	line := diffBar('+', "a + b", "x.go", 40, bgDiffAdd, fgDiffAdd, 12, 3)
	// Syntax highlighting emits multiple \033[0m resets; each must re-arm the bar
	// background, so the bg sequence appears more than once and the row ends reset.
	if strings.Count(line, bgDiffAdd) < 2 {
		t.Fatalf("background not re-applied after chroma resets: %q", line)
	}
	if !strings.HasSuffix(line, ansiReset) {
		t.Fatalf("row should end with a reset: %q", line)
	}
}
