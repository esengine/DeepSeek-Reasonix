package termrender

import (
	"runtime"
	"strings"
	"testing"
)

const diffTextSample = "diff --git a/x.go b/x.go\n" +
	"index 112294521..f9fdd2c37 100644\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1 +1 @@\n" +
	"-old\n" +
	"+new\n"

func TestDiffTextRendersSections(t *testing.T) {
	rows := DiffText(diffTextSample, 80, 24)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "x.go") {
		t.Fatalf("missing the file header:\n%s", joined)
	}
	if !strings.Contains(joined, "old") || !strings.Contains(joined, "new") {
		t.Fatalf("missing the changed rows:\n%s", joined)
	}
}

// A default `git show` carries a commit header before the diff; the rows are the
// diff's, with the log header treated as preamble.
func TestDiffTextRendersGitShow(t *testing.T) {
	show := "commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n    subject\n\n" + diffTextSample
	joined := strings.Join(DiffText(show, 80, 24), "\n")
	if !strings.Contains(joined, "x.go") || !strings.Contains(joined, "new") {
		t.Fatalf("git show lost its diff:\n%s", joined)
	}
	if strings.Contains(joined, "commit 0f2a1b9c") || strings.Contains(joined, "Author:") {
		t.Fatalf("git show log header should be preamble, not a row:\n%s", joined)
	}
}

func TestDiffTextEmpty(t *testing.T) {
	if rows := DiffText("", 80, 24); rows != nil {
		t.Fatalf("empty diff produced rows: %q", rows)
	}
	if rows := DiffText("\n\n", 80, 24); rows != nil {
		t.Fatalf("blank diff produced rows: %q", rows)
	}
}

func TestDiffTextFolds(t *testing.T) {
	rows := DiffText(diffTextSample, 80, 1)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "old") || strings.Contains(joined, "new") {
		t.Fatalf("maxLines=1 should fold the changed rows away:\n%s", joined)
	}
}

func TestDiffTextRunsConfiguredFormatterVerbatim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})

	rows := DiffText(diffTextSample, 80, 24)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "+new") || !strings.Contains(joined, "-old") {
		t.Fatalf("formatter output lost the body:\n%s", joined)
	}
}
