package termrender

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// TestRenderEmpty covers the contract that empty / whitespace-only input
// returns "" — callers rely on this to skip a redraw when there's nothing
// substantive to show.
func TestRenderEmpty(t *testing.T) {
	r := NewMarkdownRenderer(80)
	for _, in := range []string{"", " ", "\n", "\t\n  \n"} {
		if got := r.Render(in); got != "" {
			t.Errorf("Render(%q) = %q, want empty", in, got)
		}
	}
}

// TestRenderConstructsRound-trip checks each major construct emits something
// styled while preserving the underlying text. We don't assert exact ANSI
// sequences (palette could shift) — only that key visible text survives and
// that we don't degrade to literal markdown.
func TestRenderConstructs(t *testing.T) {
	r := NewMarkdownRenderer(80)
	cases := []struct {
		name     string
		in       string
		contains []string
		notRaw   []string // substrings that must NOT appear (raw markdown leaking through)
	}{
		{
			name:     "heading",
			in:       "# Hello\n",
			contains: []string{"Hello"},
			notRaw:   []string{"# Hello", "## "},
		},
		{
			name:     "heading h2 drops prefix",
			in:       "## Section\n",
			contains: []string{"Section"},
			notRaw:   []string{"## ", "###"},
		},
		{
			name:     "bold",
			in:       "this is **important** text",
			contains: []string{"important", "this is", "text"},
		},
		{
			name:     "italic",
			in:       "see *here* for details",
			contains: []string{"here", "see", "for details"},
		},
		{
			name:     "code span",
			in:       "use `os.Setenv` to set",
			contains: []string{"os.Setenv", "use", "to set"},
		},
		{
			name:     "unordered list",
			in:       "- one\n- two\n- three\n",
			contains: []string{"one", "two", "three", "•"},
		},
		{
			name:     "ordered list",
			in:       "1. first\n2. second\n",
			contains: []string{"first", "second", "1.", "2."},
		},
		{
			name:     "fenced code",
			in:       "```go\nfunc main() {}\n```\n",
			contains: []string{"func main()"},
		},
		{
			name:     "thematic break",
			in:       "above\n\n---\n\nbelow",
			contains: []string{"above", "below", "─"},
		},
		{
			name:     "gfm table",
			in:       "| name | size |\n|------|------|\n| a    | 12   |\n| bb   | 345  |\n",
			contains: []string{"name", "size", "a", "12", "bb", "345", "│"},
			notRaw:   []string{"|------|"}, // raw separator must be transformed
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := r.Render(tc.in)
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Errorf("Render(%q) missing %q\n--- output ---\n%s", tc.in, want, out)
				}
			}
			for _, leak := range tc.notRaw {
				if strings.Contains(out, leak) {
					t.Errorf("Render(%q) leaked raw markdown %q", tc.in, leak)
				}
			}
		})
	}
}

// TestWrapAnsiCJK proves the wrap counter treats CJK as 2 cols, so a line of
// Chinese characters wraps at half the column count.
func TestWrapAnsiCJK(t *testing.T) {
	// Width 10 = room for 5 Chinese characters per row.
	in := strings.Repeat("中", 8)
	out := wrapAnsi(in, 10)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrap, got 1 line: %q", out)
	}
	if VisibleWidth(lines[0]) > 10 {
		t.Errorf("first line exceeds width: %d > 10", VisibleWidth(lines[0]))
	}
}

// An ordered list counts from the number it was written with, so one that
// resumes after a code block does not restart at 1.
func TestOrderedListKeepsItsStartNumber(t *testing.T) {
	out := ansi.Strip(RenderMarkdown("3. third\n4. fourth\n", 40, false))
	if !strings.Contains(out, "3. third") || !strings.Contains(out, "4. fourth") {
		t.Fatalf("list renumbered:\n%s", out)
	}
}

// hideRail draws the fenced-code gutter as spaces so a terminal that owns the
// mouse gets clean copy with no "│" rail.
func TestRenderMarkdownHideRail(t *testing.T) {
	const src = "```\ncode\n```\n"
	if got := ansi.Strip(RenderMarkdown(src, 40, false)); !strings.Contains(got, "│") {
		t.Fatalf("default render should keep the rail:\n%q", got)
	}
	got := ansi.Strip(RenderMarkdown(src, 40, true))
	if strings.Contains(got, "│") {
		t.Fatalf("hideRail render should drop the rail:\n%q", got)
	}
	if !strings.Contains(got, "code") {
		t.Fatalf("hideRail render should keep the code:\n%q", got)
	}
}

// TestRenderDiffFence proves a ```diff fence renders through the colourised
// diff path (add/remove backgrounds) instead of the generic code rail once
// [cli].diff_fences opts in.
func TestRenderDiffFence(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	out := r.Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if strings.Contains(out, "│ ") {
		t.Fatalf("diff fence should not use the code rail:\n%s", out)
	}
	for _, want := range []string{bgDiffAdd, bgDiffDel} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff fence missing background %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "new") || !strings.Contains(out, "old") {
		t.Fatalf("diff fence dropped content:\n%s", out)
	}
	if !strings.Contains(out, "x.go") {
		t.Fatalf("diff fence header should name the file:\n%s", out)
	}
	if strings.Contains(out, "--- a/x.go") || strings.Contains(out, "+++ b/x.go") {
		t.Fatalf("diff fence should drop the raw file-header pair:\n%s", out)
	}
}

// enableDiffFences turns [cli].diff_fences on for a test and returns the
// restore func.
func enableDiffFences(t *testing.T) func() {
	t.Helper()
	prev := activeDiffFences
	activeDiffFences = true
	return func() { activeDiffFences = prev }
}

// TestRenderDiffFenceOffByDefault proves a ```diff fence stays on the plain
// code rail when the opt-in is unset — the lossless default.
func TestRenderDiffFenceOffByDefault(t *testing.T) {
	defer func(prev bool) { activeDiffFences = prev }(activeDiffFences)
	activeDiffFences = false

	out := NewMarkdownRenderer(80).Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if !strings.Contains(out, "│ ") {
		t.Fatalf("default render should keep the code rail:\n%q", out)
	}
	if strings.Contains(out, bgDiffAdd) || strings.Contains(out, bgDiffDel) {
		t.Fatalf("default render should not colourise the diff:\n%q", out)
	}
	for _, want := range []string{"-old", "+new"} {
		if !strings.Contains(out, want) {
			t.Fatalf("default render dropped %q:\n%q", want, out)
		}
	}
}

// TestRenderDiffFenceHeaderlessKeepsContent proves a fence with no "--- "/"+++ "
// pair — a bare "@@ …" hunk, or just a pair of changed lines — falls back to the
// plain rail instead of rendering as an empty block.
func TestRenderDiffFenceHeaderlessKeepsContent(t *testing.T) {
	defer enableDiffFences(t)()
	for _, body := range []string{
		"@@ -1,2 +1,2 @@\n ctx\n-old\n+new\n",
		"-old\n+new\n",
	} {
		out := NewMarkdownRenderer(80).Render("```diff\n" + body + "```\n")
		for _, want := range []string{"old", "new"} {
			if !strings.Contains(out, want) {
				t.Fatalf("headerless diff fence %q dropped %q:\n%q", body, want, out)
			}
		}
		if !strings.Contains(out, "│ ") {
			t.Fatalf("headerless diff fence %q should fall back to the rail:\n%q", body, out)
		}
	}
}

// TestRenderDiffFenceMultiFile proves a git-style fence with several files gets
// one path header per file, with the git preamble stripped from the rows.
func TestRenderDiffFenceMultiFile(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	out := r.Render("```diff\n" +
		"diff --git a/one.go b/one.go\nindex 111..222 100644\n--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/two.go b/two.go\nindex 333..444 100644\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-gone\n+kept\n" +
		"```\n")
	if n := strings.Count(out, "one.go"); n != 1 {
		t.Fatalf("want one header naming one.go, got %d occurrences:\n%s", n, out)
	}
	if n := strings.Count(out, "two.go"); n != 1 {
		t.Fatalf("want one header naming two.go, got %d occurrences:\n%s", n, out)
	}
	for _, leak := range []string{"diff --git", "index 111", "index 333"} {
		if strings.Contains(out, leak) {
			t.Fatalf("git preamble %q leaked into the rows:\n%s", leak, out)
		}
	}
}

// TestDiffSectionStartNeedsHunkAfterHeaderPair proves a removed "-- x" line
// (rendered "--- x") followed by an added "++ y" line (rendered "+++ y") is not
// taken for a file header: only the "--- "/"+++ " pair a "@@ " hunk follows is
// one, so a section cannot open on a pair of changed comment rows.
func TestDiffSectionStartNeedsHunkAfterHeaderPair(t *testing.T) {
	lines := strings.Split("@@ -1 +1 @@\n--- old\n+++ new\n ctx\n", "\n")
	if diffSectionStart(lines, 1, false) {
		t.Fatalf("a removed/added comment pair must not open a section: %q", lines[1:3])
	}
	lines = strings.Split("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n", "\n")
	if !diffSectionStart(lines, 0, false) {
		t.Fatal("a real file header pair must open a section")
	}
}

// TestTrimDiffPreambleKeepsHeaderlessHunk proves a headerless hunk that opens
// with a removed "-- x" line is not trimmed: trimDiffPreamble only accepts the
// pair a "@@ " hunk follows, so nothing is dropped and the hunk falls back to
// the plain rail whole.
func TestTrimDiffPreambleKeepsHeaderlessHunk(t *testing.T) {
	sec := "@@ -1,2 +1,2 @@\n--- old comment\n ctx\n+added\n"
	if got := trimDiffPreamble(sec); got != "" {
		t.Fatalf("trimDiffPreamble(%q) = %q, want empty (no file header)", sec, got)
	}
	sec = "diff --git a/x.sql b/x.sql\nindex 1..2 100644\n--- a/x.sql\n+++ b/x.sql\n@@ -1 +1 @@\n-old\n+new\n"
	if got := trimDiffPreamble(sec); !strings.HasPrefix(got, "--- a/x.sql\n") {
		t.Fatalf("trimDiffPreamble lost a real file header: %q", got)
	}
}

// TestCountDiffCountsRemovedCommentRows proves a removed "-- x" line (rendered
// "--- x") and an added "++ y" line (rendered "+++ y") inside a hunk are counted
// as changes, not skipped as a file-header pair.
func TestCountDiffCountsRemovedCommentRows(t *testing.T) {
	d := countDiff("--- a/x.sql\n+++ b/x.sql\n@@ -1,2 +1,2 @@\n--- old comment\n+-- new comment\n ctx\n")
	if d.Added != 1 || d.Removed != 1 {
		t.Fatalf("countDiff = +%d -%d, want +1 -1", d.Added, d.Removed)
	}
}

// TestRenderDiffFenceHeaderlessCommentKeepsContent proves a ```diff fence whose
// hunk opens with a removed comment line keeps every row — the "@@ " header and
// both comment rows — instead of dropping the rows before the "--- x" line.
func TestRenderDiffFenceHeaderlessCommentKeepsContent(t *testing.T) {
	defer enableDiffFences(t)()
	for _, tc := range []struct {
		body string
		want []string
	}{
		{"@@ -1,2 +1,2 @@\n--- old comment\n ctx\n+added\n", []string{"@@ ", "--- old comment", " ctx", "+added"}},
		{"@@ -1 +1 @@\n--- old\n+++ new\n", []string{"@@ ", "--- old", "+++ new"}},
	} {
		out := NewMarkdownRenderer(80).Render("```diff\n" + tc.body + "```\n")
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Fatalf("headerless hunk %q dropped %q:\n%q", tc.body, want, out)
			}
		}
	}
}

// TestRenderDiffFencePendingFormatterDrawsPlainRail proves a ```diff fence whose
// configured formatter result is still pending and has no landed prefix is drawn
// on the plain code rail: the formatter's output supersedes the built-in rows,
// so colourising them would only be thrown away. A single-hunk open fence has no
// settled prefix, so the run is deterministically pending.
func TestRenderDiffFencePendingFormatterDrawsPlainRail(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest([]string{"cat"})
	defer restore()
	SetDiffFormatNotify(func(DiffKey) {})
	defer SetDiffFormatNotify(nil)

	out := NewMarkdownRenderer(80).Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n")
	if strings.Contains(out, bgDiffAdd) || strings.Contains(out, bgDiffDel) {
		t.Fatalf("a pending formatter should not colourise the built-in rows:\n%q", out)
	}
	if !strings.Contains(out, "│ ") || !strings.Contains(out, "old") || !strings.Contains(out, "new") {
		t.Fatalf("a pending formatter should keep the fence on the plain rail:\n%q", out)
	}
}

// TestRenderDiffFenceLandedFormatterFailureFallsBackToBuiltin proves the built-in
// colourised rows are still drawn once the formatter has landed and failed — the
// plain rail is only the pending placeholder, not the fallback.
func TestRenderDiffFenceLandedFormatterFailureFallsBackToBuiltin(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	// A command that produces no output fails the run inline (notify nil), so the
	// memo holds a failed entry by the time the built-in rows are chosen.
	restore := SetDiffFormatterForTest([]string{"false"})
	defer restore()
	SetDiffFormatNotify(nil)

	out := NewMarkdownRenderer(80).Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if !strings.Contains(out, bgDiffAdd) || !strings.Contains(out, bgDiffDel) {
		t.Fatalf("a failed formatter should fall back to the colourised built-in rows:\n%q", out)
	}
}

// TestRenderDiffFenceOpenDrawsPlainRail proves a single-hunk still-streaming
// fence stays on the plain rail even with no formatter configured: no hunk has
// settled, so there is no prefix to colourise. The colourised rows are drawn
// once the closing fence arrives.
func TestRenderDiffFenceOpenDrawsPlainRail(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)

	open := "```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"
	out := NewMarkdownRenderer(80).Render(open)
	if strings.Contains(out, bgDiffAdd) || strings.Contains(out, bgDiffDel) {
		t.Fatalf("an open fence should not be colourised:\n%q", out)
	}
	if !strings.Contains(out, "│ ") || !strings.Contains(out, "old") || !strings.Contains(out, "new") {
		t.Fatalf("an open fence should stay on the plain rail:\n%q", out)
	}

	closed := NewMarkdownRenderer(80).Render(open + "```\n")
	if !strings.Contains(closed, bgDiffAdd) || !strings.Contains(closed, bgDiffDel) {
		t.Fatalf("a closed fence should be colourised:\n%q", closed)
	}
}

// TestSplitAtLastHunk proves the settled/in-progress cut: a hunk header settles
// everything before it, but only once an earlier hunk has finished.
func TestSplitAtLastHunk(t *testing.T) {
	for _, tc := range []struct {
		name, text, settled, tail string
	}{
		{"no hunk", "--- a/x\n+++ b/x\n-old\n", "", "--- a/x\n+++ b/x\n-old\n"},
		{"one hunk", "@@ -1 +1 @@\n-old\n+new\n", "", "@@ -1 +1 @@\n-old\n+new\n"},
		{"two hunks", "@@ -1 +1 @@\n-old\n+new\n@@ -9 +9 @@\n-still\n", "@@ -1 +1 @@\n-old\n+new", "@@ -9 +9 @@\n-still\n"},
		{
			"next file header trails into the tail",
			"@@ -1 +1 @@\n-a\n+b\n@@ -5 +5 @@\n-c\n+d\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-e\n+f\n",
			"@@ -1 +1 @@\n-a\n+b\n@@ -5 +5 @@\n-c\n+d",
			"--- a/y\n+++ b/y\n@@ -1 +1 @@\n-e\n+f\n",
		},
		{
			"git preamble before the dangling pair also moves",
			"@@ -1 +1 @@\n-a\n+b\n@@ -5 +5 @@\n-c\n+d\ndiff --git a/y b/y\nindex 1..2 100644\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-e\n+f\n",
			"@@ -1 +1 @@\n-a\n+b\n@@ -5 +5 @@\n-c\n+d",
			"diff --git a/y b/y\nindex 1..2 100644\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-e\n+f\n",
		},
	} {
		settled, tail := splitAtLastHunk(tc.text)
		if settled != tc.settled || tail != tc.tail {
			t.Errorf("%s: settled=%q tail=%q, want %q / %q", tc.name, settled, tail, tc.settled, tc.tail)
		}
	}
}

// TestRenderDiffFenceColourisesAtHunkBoundary proves a still-open fence is
// colourised up to its last "@@ " header: the completed hunk shows coloured
// while the in-progress one stays on the plain rail, so the diff colours in as
// it streams instead of only at the closing fence.
func TestRenderDiffFenceColourisesAtHunkBoundary(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)

	open := "```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n@@ -9 +9 @@\n-still\n"
	out := NewMarkdownRenderer(80).Render(open)
	if !strings.Contains(out, bgDiffAdd) {
		t.Fatalf("the completed hunk should be colourised:\n%q", out)
	}
	stripped := ansi.Strip(out)
	if !strings.Contains(stripped, "│ @@ -9 +9 @@") || !strings.Contains(stripped, "│ -still") {
		t.Fatalf("the in-progress hunk should stay on the plain rail:\n%q", out)
	}
}

// TestStreamingDiffHunkMemoisedOnce proves a hunk memoizes to one entry whether
// it renders as a section's last hunk (carrying the section's trailing newline)
// or once a following hunk exists. Keying on the raw text would give one hunk
// two keys and highlight it twice.
func TestStreamingDiffHunkMemoisedOnce(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)
	resetBuiltinDiffCache()
	defer resetBuiltinDiffCache()

	full := "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old1\n+new1\n@@ -9 +9 @@\n-old2\n+new2\n"
	r := NewMarkdownRenderer(80)
	var sb strings.Builder
	sb.WriteString("```diff\n")
	for _, ln := range strings.SplitAfter(full, "\n") {
		if ln == "" {
			continue
		}
		sb.WriteString(ln)
		_ = r.Render(sb.String())
	}
	sb.WriteString("```\n")
	_ = r.Render(sb.String())
	if got := len(builtinDiffCache); got != 2 {
		t.Fatalf("two hunks should memoize to 2 entries, got %d", got)
	}
}

// TestRenderDiffFenceMultiHunkSection proves a file section's hunks are each
// colourised and separated by the "⋮" jump marker, the same rows a single
// whole-body render produced.
func TestRenderDiffFenceMultiHunkSection(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)

	out := NewMarkdownRenderer(80).Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n@@ -9 +9 @@\n-gone\n+kept\n```\n")
	if !strings.Contains(out, bgDiffAdd) || !strings.Contains(out, bgDiffDel) {
		t.Fatalf("both hunks should be colourised:\n%q", out)
	}
	if !strings.Contains(ansi.Strip(out), "⋮") {
		t.Fatalf("the hunks should be separated by the jump marker:\n%q", out)
	}
	for _, want := range []string{"old", "new", "gone", "kept"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q:\n%q", want, out)
		}
	}
}

// TestRenderDiffFenceMultiFileStreamHeaderStaysPlain proves that while a
// multi-file diff streams, the next file's "--- "/"+++ " header is not drawn as
// a removed/added row of the previous file's last hunk: until that file's hunk
// completes, its header rides the plain rail and the previous file's stat is not
// inflated by it.
func TestRenderDiffFenceMultiFileStreamHeaderStaysPlain(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)

	full := "--- a/x.ts\n+++ b/x.ts\n" +
		"@@ -1 +1 @@\n-a1\n+b1\n" +
		"@@ -5 +5 @@\n-a2\n+b2\n" +
		"--- a/y.ts\n+++ b/y.ts\n" +
		"@@ -1 +1 @@\n-c1\n+d1\n" +
		"@@ -9 +9 @@\n-c2\n+d2\n"
	// Pause just after the second file's first "@@ " line: its header pair is
	// present but no hunk of it has completed.
	cut := strings.Index(full, "@@ -1 +1 @@\n-c1\n") + len("@@ -1 +1 @@\n-c1\n")
	out := NewMarkdownRenderer(80).Render("```diff\n" + full[:cut])

	stripped := ansi.Strip(out)
	if !strings.Contains(stripped, "│ --- a/y.ts") || !strings.Contains(stripped, "│ +++ b/y.ts") {
		t.Fatalf("the next file's header should stay on the plain rail:\n%s", stripped)
	}
	if !strings.Contains(stripped, "x.ts  +2 -2") {
		t.Fatalf("the first file's stat should not count the dangling header:\n%s", stripped)
	}

	// Once the second file's last hunk header arrives, its section is complete
	// and gets its own path header.
	closed := NewMarkdownRenderer(80).Render("```diff\n" + full + "```\n")
	if !strings.Contains(ansi.Strip(closed), "y.ts  +2 -2") {
		t.Fatalf("the second file should get its own header once complete:\n%s", ansi.Strip(closed))
	}
}

// TestRenderDiffFenceThemeSwitchRepaintsHunks proves the per-hunk memo is keyed
// by the active theme: rendering the same fence under a second palette must
// recolour its rows rather than serve the first palette's cached ones.
func TestRenderDiffFenceThemeSwitchRepaintsHunks(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer func(prev Palette) { activeTheme = prev }(activeTheme)
	defer enableDiffFences(t)()
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)

	fence := "```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n```\n"
	activeTheme = cliDarkTheme
	dark := NewMarkdownRenderer(80).Render(fence)
	activeTheme = cliLightTheme
	light := NewMarkdownRenderer(80).Render(fence)

	if dark == light {
		t.Fatalf("switching theme should change the rendered bytes:\n%q", light)
	}
	if strings.Contains(light, bgSGR(cliDarkTheme.DiffAddBG)) {
		t.Fatalf("the light render served the dark palette's cached hunk rows:\n%q", light)
	}
	if !strings.Contains(light, bgSGR(cliLightTheme.DiffAddBG)) {
		t.Fatalf("the light render is missing the light palette's diff background:\n%q", light)
	}
}
