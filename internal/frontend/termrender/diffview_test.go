package termrender

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/event"
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
	for range 8 {
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
	block := DiffBlock("edit_file", `{"path":"pkg/x.go"}`, d, 80, 40)
	if len(block) == 0 || !strings.Contains(block[0], "Update") || !strings.Contains(block[0], "pkg/x.go") {
		t.Fatalf("header should name verb + path, got %q", block[0])
	}
}

func TestDiffBlockNilWithoutDiff(t *testing.T) {
	if DiffBlock("write_file", `{"path":"x"}`, event.FileDiff{}, 80, 40) != nil {
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
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256

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

func TestActiveDiffChromaStyleFollowsCLITheme(t *testing.T) {
	previous := activeTheme
	defer func() { activeTheme = previous }()

	tests := []struct {
		name  string
		theme Palette
		want  string
	}{
		{name: "dark", theme: cliDarkTheme, want: "github-dark"},
		{name: "light", theme: cliLightTheme, want: "github"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeTheme = tt.theme
			if got := activeDiffChromaStyle().Name; got != tt.want {
				t.Fatalf("diff syntax style = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHighlightClampedCarriesNoBackground(t *testing.T) {
	previousTheme := activeTheme
	previousProfile := activeColorProfile
	defer func() {
		activeTheme = previousTheme
		activeColorProfile = previousProfile
	}()
	SetColorProfile(colorprofile.ANSI256)
	activeTheme = cliLightTheme

	// A clamped added line that ends inside an open string literal. Highlighting
	// after clamping lexes the lone quote as an Error token, whose light-theme
	// background would paint over the diff bar's own colour.
	code := "\tfmt.Fprintln(os.Stderr, \"hello, this is a deliberately long added line\")"
	got := highlightClamped(code, "main.go", 40)
	if strings.Contains(got, "\x1b[48;") {
		t.Fatalf("clamped line must carry no background: %q", got)
	}
	if w := VisibleWidth(got); w > 40 {
		t.Fatalf("clamped line width = %d, want <= 40: %q", w, got)
	}
}

func TestHighlightCodeUpdatesOnThemeSwitch(t *testing.T) {
	previousTheme := activeTheme
	previousProfile := activeColorProfile
	defer func() {
		activeTheme = previousTheme
		activeColorProfile = previousProfile
	}()
	activeColorProfile = colorprofile.ANSI256

	code := `const answer = "value"`
	activeTheme = cliLightTheme
	light := highlightCode("example.ts", code)
	activeTheme = cliDarkTheme
	dark := highlightCode("example.ts", code)

	if light == dark {
		t.Fatalf("light and dark themes produced identical highlighting: %q", light)
	}
	for name, got := range map[string]string{"light": light, "dark": dark} {
		if plain := ansi.Strip(got); plain != code {
			t.Fatalf("%s theme changed code text: got %q, want %q", name, plain, code)
		}
	}
}

// TestDiffBodyFoldsLargePreview proves a folded preview keeps maxLines rows
// however large the diff, reporting the rest: the fold is counted from the
// source, so the tail is never laid out. Four hunks add three "⋮" separators,
// so 43 rows fold to 5 kept + a footer naming 38.
func TestDiffBodyFoldsLargePreview(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- a/x\n+++ b/x\n")
	for h := range 4 {
		fmt.Fprintf(&b, "@@ -%d,10 +%d,10 @@\n", h*10+1, h*10+1)
		for range 10 {
			b.WriteString("+line\n")
		}
	}
	body := diffBody(event.FileDiff{Diff: b.String()}, "x", 80, 6)
	if len(body) != 6 {
		t.Fatalf("want 6 rows (5 kept + footer), got %d:\n%s", len(body), strings.Join(body, "\n"))
	}
	if !strings.Contains(body[len(body)-1], "38") {
		t.Fatalf("footer should report 38 folded rows, got %q", body[len(body)-1])
	}
}

// TestDiffBodyFoldSeamSameRows proves the benchmark's seam is a fair arm: with
// the fold off — every row laid out and the tail dropped after — diffBody draws
// the same rows as the production path, so the benchmark prices the same work.
func TestDiffBodyFoldSeamSameRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- a/x\n+++ b/x\n")
	for h := range 4 {
		fmt.Fprintf(&b, "@@ -%d,10 +%d,10 @@\n", h*10+1, h*10+1)
		for range 10 {
			b.WriteString("+line\n")
		}
	}
	d := event.FileDiff{Diff: b.String()}

	defer func(prev bool) { diffPreviewFold = prev }(diffPreviewFold)
	diffPreviewFold = true
	on := strings.Join(diffBody(d, "x", 80, 6), "\n")
	diffPreviewFold = false
	off := strings.Join(diffBody(d, "x", 80, 6), "\n")

	if on != off {
		t.Fatalf("fold seam changed the rows:\non:  %q\noff: %q", on, off)
	}
}
