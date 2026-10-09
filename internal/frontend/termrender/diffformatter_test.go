package termrender

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"reasonix/internal/contract/event"
)

func TestSplitDiffFormatter(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"delta --color-only --paging=never", []string{"delta", "--color-only", "--paging=never"}},
		{`delta --syntax-theme "Monokai Extended"`, []string{"delta", "--syntax-theme", "Monokai Extended"}},
		{`my tool 'a b' c`, []string{"my", "tool", "a b", "c"}},
		{`x ""`, []string{"x", ""}},
	}
	for _, tc := range cases {
		if got := splitDiffFormatter(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitDiffFormatter(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestRenderDiffExternalPassesStdinToStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	out, ok := renderDiffExternal([]string{"cat"}, "--- a/x\n+++ b/x\n", 0)
	if !ok {
		t.Fatal("renderDiffExternal(cat) failed")
	}
	if out != "--- a/x\n+++ b/x\n" {
		t.Fatalf("stdout = %q", out)
	}
}

// A width-aware formatter sees COLUMNS, and its output is clamped to width so a
// formatter that ignores COLUMNS cannot overflow the viewport.
func TestRenderDiffExternalPassesWidthAndClamps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	// `cat` ignores COLUMNS; the over-wide line is clamped to 10 columns.
	out, ok := renderDiffExternal([]string{"cat"}, strings.Repeat("x", 200)+"\n", 10)
	if !ok {
		t.Fatal("renderDiffExternal(cat) failed")
	}
	if got := VisibleWidth(strings.TrimRight(out, "\n")); got != 10 {
		t.Fatalf("over-wide output not clamped: width %d (%q)", got, out)
	}
}

// A formatter that exits but leaves a child holding stdout must not block Wait
// for the child's lifetime: cmd.WaitDelay bounds the run (and the misbehaving
// formatter is rejected, so the caller falls back to the built-in renderer).
func TestRenderDiffExternalBoundsAChildHoldingStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable shell")
	}
	start := time.Now()
	_, _ = renderDiffExternal([]string{"sh", "-c", "sleep 5 & echo done"}, "diff\n", 80)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("renderDiffExternal blocked %v on a child holding stdout", elapsed)
	}
}

func TestRenderDiffExternalFallsBackOnError(t *testing.T) {
	if _, ok := renderDiffExternal([]string{"reasonix-no-such-command-xyz"}, "diff", 80); ok {
		t.Fatal("expected ok=false for a missing command")
	}
	if _, ok := renderDiffExternal(nil, "diff", 80); ok {
		t.Fatal("expected ok=false for empty argv")
	}
	if _, ok := renderDiffExternal([]string{"cat"}, "", 80); ok {
		t.Fatal("expected ok=false for empty diff")
	}
	if _, ok := renderDiffExternal([]string{"cat"}, strings.Repeat("x", diffFormatMaxBytes+1), 80); ok {
		t.Fatal("expected ok=false for an oversized diff")
	}
}

func TestDiffBodyVerbatimWhenColourised(t *testing.T) {
	// SGR-coloured input is shown as-is (no line-number gutter added).
	d := event.FileDiff{Diff: "\x1b[32m+added\x1b[0m\n\x1b[31m-removed\x1b[0m\n"}
	rows := diffBody(d, "x.go", 80, 0)
	if len(rows) != 2 {
		t.Fatalf("want 2 verbatim rows, got %d: %#v", len(rows), rows)
	}
	for _, r := range rows {
		if !strings.Contains(r, "\x1b[") {
			t.Fatalf("verbatim row lost its colour: %q", r)
		}
		if strings.Contains(r, "\x1b[48;5;") {
			t.Fatalf("verbatim row should not carry a background bar: %q", r)
		}
	}
}

func TestSGROnlyKeepsColourDropsHijack(t *testing.T) {
	in := "\x1b[32mgreen\x1b[0m\x1b]52;c;aGk=\x07\x1b[2Jtext"
	got := sgrOnly(in)
	if !strings.Contains(got, "\x1b[32m") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("SGR dropped: %q", got)
	}
	if strings.Contains(got, "\x1b]") || strings.Contains(got, "\x1b[2J") {
		t.Fatalf("non-SGR control sequence leaked: %q", got)
	}
	if !strings.Contains(got, "green") || !strings.Contains(got, "text") {
		t.Fatalf("visible text lost: %q", got)
	}
}

func TestRenderDiffFenceVerbatimWhenColourised(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer enableDiffFences(t)()
	r := NewMarkdownRenderer(80)
	out := r.Render("```diff\n\x1b[32m+added\x1b[0m\n\x1b[31m-removed\x1b[0m\n```\n")
	if !strings.Contains(out, "\x1b[32m+added") || !strings.Contains(out, "\x1b[31m-removed") {
		t.Fatalf("coloured diff fence lost its SGR:\n%q", out)
	}
	if strings.Contains(out, "\x1b[48;5;") {
		t.Fatalf("coloured diff fence should not be re-parsed into background bars:\n%q", out)
	}
}

// A configured [cli].diff_formatter runs the whole fence body through the
// command and its stdout is re-emitted under the fence — the formatter's SGR
// survives, but a non-SGR control sequence it emitted does not.
func TestRenderDiffFenceSanitisesConfiguredFormatter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	body := "--- a/x\n+++ b/x\n\x1b]52;c;aGk=\x07\x1b[31m-red\x1b[0m\n"
	out := r.Render("```diff\n" + body + "```\n")
	if strings.Contains(out, "\x1b]52;c;aGk=\x07") {
		t.Fatalf("formatter's OSC was not filtered:\n%q", out)
	}
	if !strings.Contains(out, "\x1b[31m-red\x1b[0m") {
		t.Fatalf("formatter output lost its SGR:\n%q", out)
	}
	if strings.Contains(out, "\x1b[48;5;") {
		t.Fatalf("formatted fence should not be re-rendered with background bars:\n%q", out)
	}
}

// An open fence with fewer than two hunks never consults the external formatter:
// with no settled prefix every streamed delta would spawn a subprocess that can
// never hit the memo, so the plain rail is drawn until the closing fence.
func TestRenderDiffFenceOpenFenceSkipsFormatter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	// A formatter that marks every line; the marker only appears once closed.
	setDiffFormatter([]string{"sed", "s/^/MARK:/"})
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	open := r.Render("```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n")
	if strings.Contains(open, "MARK:") {
		t.Fatalf("open fence ran the external formatter:\n%q", open)
	}
	if !strings.Contains(open, "old") || !strings.Contains(open, "new") {
		t.Fatalf("open fence dropped content:\n%q", open)
	}
	closed := r.Render("```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if !strings.Contains(closed, "MARK:") {
		t.Fatalf("closed fence skipped the external formatter:\n%q", closed)
	}
}

// A single-hunk fence runs the formatter once, on the closed fence — never per
// delta. (A multi-hunk fence runs it once per completed hunk; see
// TestRenderDiffFenceFormatterRunsOnSettledHunk.)
func TestRenderDiffFenceStreamingRunsFormatterOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable shell")
	}
	countFile := filepath.Join(t.TempDir(), "runs")
	t.Setenv("COUNTFILE", countFile)
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"sh", "-c", `echo run >> "$COUNTFILE"; cat`})
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	full := "```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	for i := 1; i <= len(full); i++ {
		r.Render(full[:i])
	}
	if data, err := os.ReadFile(countFile); err == nil && len(data) != 0 {
		t.Fatalf("formatter ran on open fences:\n%s", data)
	}
	r.Render(full + "```\n")
	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("formatter never ran: %v", err)
	}
	if got := strings.Count(string(data), "\n"); got != 1 {
		t.Fatalf("formatter ran %d times, want 1", got)
	}
}

// Without a configured formatter the fence keeps the built-in renderer.
func TestRenderDiffFenceFallsBackWithoutFormatter(t *testing.T) {
	defer func(prev []string) { activeDiffFormatter = prev }(activeDiffFormatter)
	activeDiffFormatter = nil
	defer enableDiffFences(t)()

	r := NewMarkdownRenderer(80)
	out := r.Render("```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if !strings.Contains(out, "new") || !strings.Contains(out, "old") {
		t.Fatalf("built-in renderer lost the diff body:\n%q", out)
	}
}

// A configured [cli].diff_formatter also formats a writer tool's diff card: the
// whole diff is piped through the command and its stdout is re-emitted under
// the card header, SGR kept and other escapes stripped, with no fold (maxLines
// is ignored), mirroring the fenced path.
func TestDiffBlockSanitisesConfiguredFormatter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})

	d := event.FileDiff{
		Diff:    "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n\x1b]52;c;aGk=\x07\x1b[31m-old\x1b[0m\n+new\n",
		Added:   1,
		Removed: 1,
	}
	block := DiffBlock("edit_file", `{"path":"pkg/x.go"}`, d, 80, 1)
	if len(block) == 0 || !strings.Contains(block[0], "pkg/x.go") {
		t.Fatalf("header should name the path, got %q", block[0])
	}
	body := strings.Join(block[1:], "\n")
	if strings.Contains(body, "\x1b]52;c;aGk=\x07") {
		t.Fatalf("formatter's OSC was not filtered:\n%q", body)
	}
	// The raw "--- /+++ " pair the built-in path drops must not reach the
	// formatter either, or the card re-emits a host path it already names.
	if strings.Contains(body, "--- a/x.go") || strings.Contains(body, "+++ b/x.go") {
		t.Fatalf("formatter path should drop the raw file headers:\n%q", body)
	}
	if !strings.Contains(body, "\x1b[31m-old\x1b[0m") {
		t.Fatalf("formatter output lost its body:\n%q", body)
	}
	if strings.Contains(body, "\x1b[48;5;") {
		t.Fatalf("formatted card should not be re-rendered with background bars:\n%q", body)
	}
	if strings.Contains(body, "more lines") {
		t.Fatalf("formatted card should not fold even past maxLines:\n%q", body)
	}
	for _, row := range block[1:] {
		if !strings.HasPrefix(row, "  ") {
			t.Fatalf("body row should be indented to the card column, got %q", row)
		}
	}
}

// Without a configured formatter the writer diff card keeps the built-in
// renderer (headers dropped, line-number gutter added).
func TestDiffBlockFallsBackWithoutFormatter(t *testing.T) {
	defer func(prev []string) { activeDiffFormatter = prev }(activeDiffFormatter)
	activeDiffFormatter = nil

	d := event.FileDiff{Diff: "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n", Added: 1, Removed: 1}
	body := strings.Join(DiffBlock("edit_file", `{"path":"x.go"}`, d, 80, 40)[1:], "\n")
	if !strings.Contains(body, "old") || !strings.Contains(body, "new") {
		t.Fatalf("built-in renderer lost the diff body:\n%q", body)
	}
	if strings.Contains(body, "--- a/x.go") || strings.Contains(body, "+++ b/x.go") {
		t.Fatalf("built-in renderer should drop the file headers:\n%q", body)
	}
}

// A formatter that fails to run leaves the writer diff card on the built-in
// renderer.
func TestDiffBlockFallsBackWhenFormatterFails(t *testing.T) {
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"reasonix-no-such-command-xyz"})

	d := event.FileDiff{Diff: "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n", Added: 1, Removed: 1}
	body := strings.Join(DiffBlock("edit_file", `{"path":"x.go"}`, d, 80, 40)[1:], "\n")
	if !strings.Contains(body, "old") || !strings.Contains(body, "new") {
		t.Fatalf("built-in renderer lost the diff body:\n%q", body)
	}
}

// setDiffFormatter installs a formatter for a test and drops the memo so a case
// never reads a result another case cached.
func setDiffFormatter(argv []string) { SetDiffFormatterForTest(argv) }

// With a re-render hook registered the run is asynchronous: the call returns
// not-ok at once (so the caller draws its placeholder rows), the hook fires when
// the result lands, and the memo then serves it.
func TestFormatDiffCachedAsyncWhenNotifyHookSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})

	done := make(chan struct{}, 1)
	SetDiffFormatNotify(func(DiffKey) { done <- struct{}{} })
	defer SetDiffFormatNotify(nil)

	if _, ok := formatDiffCached("body", 80); ok {
		t.Fatal("async call should report not-ok so the caller draws placeholder rows")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notify hook never fired")
	}
	if got, ok := formatDiffCached("body", 80); !ok || got != "body" {
		t.Fatalf("memo not populated after the run landed: %q, %v", got, ok)
	}
}

// The memo answers a repeated (content, width) without spawning the command
// again, and a new width is a new key.
func TestFormatDiffCachedMemoisesByContentAndWidth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})

	if got, ok := formatDiffCached("body", 80); !ok || got != "body" {
		t.Fatalf("formatDiffCached = %q, %v", got, ok)
	}
	// A command that cannot run: a cache hit must not consult it.
	activeDiffFormatter = []string{"reasonix-no-such-command-xyz"}
	if got, ok := formatDiffCached("body", 80); !ok || got != "body" {
		t.Fatalf("cache miss re-ran the formatter: %q, %v", got, ok)
	}
	if _, ok := formatDiffCached("body", 100); ok {
		t.Fatal("a new width should not reuse another width's entry")
	}
}

// A multi-hunk fence runs the formatter on each completed hunk as it arrives,
// not only at the closing fence, so the diff colours in as it streams. The
// in-progress hunk is left on the plain rail and never reaches the formatter.
func TestRenderDiffFenceFormatterRunsOnSettledHunk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"sed", "s/^/MARK:/"})
	SetDiffFormatNotify(nil)
	defer enableDiffFences(t)()

	open := "```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n@@ -9 +9 @@\n-still\n"
	out := NewMarkdownRenderer(80).Render(open)
	if !strings.Contains(out, "MARK:") {
		t.Fatalf("the completed hunk should be formatted while the fence is open:\n%q", out)
	}
	if strings.Contains(out, "MARK:-still") || strings.Contains(out, "MARK:@@ -9") {
		t.Fatalf("the in-progress hunk must not reach the formatter:\n%q", out)
	}
}

// A settled body grows by one hunk each time an "@@ " header arrives, so its
// formatter key changes and its run goes pending again. While that run is in
// flight the rows already formatted must keep the formatter's output: redrawing
// the whole section on the plain rail made the delta output flash back to plain
// at every hunk. Only the newly settled hunk and the in-progress one wait.
func TestRenderDiffFenceKeepsFormattedPrefixWhileGrownBodyPending(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	defer setDiffFormatter(nil)
	setDiffFormatter([]string{"cat"})
	SetDiffFormatNotify(func(DiffKey) {}) // async: a miss reads as pending
	defer SetDiffFormatNotify(nil)
	defer enableDiffFences(t)()

	const (
		header = "--- a/x\n+++ b/x\n"
		h1     = "@@ -1 +1 @@\n-aaa\n+AAA\n"
		h2     = "@@ -9 +9 @@\n-bbb\n+BBB\n"
		h3     = "@@ -20 +20 @@\n-ccc\n+CCC\n"
		h4     = "@@ -30 +30 @@\n-ddd\n" // in progress
	)
	// The prefix already settled and formatted; the body grown by h3 — the
	// current settled value — is left pending. A settled value drops the
	// newline before the next hunk, so the memo key is the trimmed prefix.
	body := header + h1 + h2
	diffFormatMu.Lock()
	storeDiffFormat(diffKeyOf(strings.TrimRight(body, "\n"), 80), "FORMATTED-PREFIX\n", true)
	diffFormatMu.Unlock()

	out := NewMarkdownRenderer(80).Render("```diff\n" + body + h3 + h4)
	if !strings.Contains(out, "FORMATTED-PREFIX") {
		t.Fatalf("the settled prefix flashed back to the plain rail:\n%q", out)
	}
	if strings.Contains(out, "│ -aaa") {
		t.Fatalf("the already-formatted prefix was redrawn on the plain rail:\n%q", out)
	}
	if !strings.Contains(out, "│ -ccc") || !strings.Contains(out, "│ -ddd") {
		t.Fatalf("the newly settled and in-progress hunks should wait on the rail:\n%q", out)
	}
}
