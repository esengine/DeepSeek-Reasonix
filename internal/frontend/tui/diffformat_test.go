package tui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/frontend/termrender"
)

// diffFormatHarness installs a stdin→stdout formatter (cat) and a re-render
// hook, so a formatter run is asynchronous exactly as the full-screen TUI makes
// it. The returned channel receives the key of each landed run.
func diffFormatHarness(t *testing.T) (landed <-chan termrender.DiffKey) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	restore := termrender.SetDiffFormatterForTest([]string{"cat"})
	ch := make(chan termrender.DiffKey, 256)
	termrender.SetDiffFormatNotify(func(k termrender.DiffKey) { ch <- k })
	t.Cleanup(func() {
		termrender.SetDiffFormatNotify(nil)
		restore()
	})
	return ch
}

// waitLanded fails unless a formatter run lands within the deadline, and returns
// the key it landed for.
func waitLanded(t *testing.T, landed <-chan termrender.DiffKey) termrender.DiffKey {
	t.Helper()
	select {
	case k := <-landed:
		return k
	case <-time.After(5 * time.Second):
		t.Fatal("formatter run never landed")
		return termrender.DiffKey{}
	}
}

// cachedRows is every settled block's rows through the cache the screen draws
// from, so a stale cache shows up exactly as it would on screen.
func cachedRows(m *model) []string {
	var rows []string
	cw, hide := m.contentWidth(), m.scrollbarHidden()
	for i := range m.scr.blocks {
		for _, l := range m.scr.blocks[i].at(cw, hide) {
			rows = append(rows, strings.TrimRight(ansi.Strip(l), " "))
		}
	}
	return rows
}

// rawRows is every settled block's unwrapped render output, so an over-wide row
// is seen as its own width rather than the wrapping the cache would apply.
func rawRows(m *model) []string {
	var rows []string
	cw := m.contentWidth()
	for i := range m.scr.blocks {
		rows = append(rows, strings.Split(m.scr.blocks[i].render(cw, m.scrollbarHidden()), "\n")...)
	}
	return rows
}

// A marked shell diff settles into a block while the formatter run is still in
// flight, so the block caches the built-in rows. When the run lands the screen
// must redraw that block with the formatted rows, not keep what it cached.
func TestLandedDiffResultReplacesTheSettledShellDiffRows(t *testing.T) {
	landed := diffFormatHarness(t)
	m, _ := testModel(t)
	apply(m, bashDiffEvent(true)...)

	before := strings.Join(cachedRows(m), "\n")
	if !strings.Contains(before, "+1 -1") {
		t.Fatalf("built-in rows not drawn while the run was in flight:\n%s", before)
	}
	key := waitLanded(t, landed)

	m.Update(diffFormattedMsg{key: key})

	after := strings.Join(cachedRows(m), "\n")
	if strings.Contains(after, "+1 -1") {
		t.Fatalf("the settled block kept its built-in rows after the run landed:\n%s", after)
	}
	if !strings.Contains(after, "--- a/x.go") {
		t.Fatalf("the formatted rows never replaced the built-in ones:\n%s", after)
	}
}

// The same staleness hits a writer call's diff card: the card is drawn before
// the run lands, and the result must reach it once it does.
func TestLandedDiffResultReplacesTheSettledWriteCardRows(t *testing.T) {
	landed := diffFormatHarness(t)
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "w1", Name: "edit_file", Args: `{"path":"x.go"}`}})
	apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "w1", Name: "edit_file", Args: `{"path":"x.go"}`, Diff: tuiDiff, Added: 1, Removed: 1}})

	before := strings.Join(cachedRows(m), "\n")
	if !strings.Contains(before, "1 - OLD_LINE") {
		t.Fatalf("built-in card body not drawn while the run was in flight:\n%s", before)
	}
	key := waitLanded(t, landed)

	m.Update(diffFormattedMsg{key: key})

	after := strings.Join(cachedRows(m), "\n")
	if strings.Contains(after, "1 - OLD_LINE") {
		t.Fatalf("the settled card kept its built-in body after the run landed:\n%s", after)
	}
	if !strings.Contains(after, "-OLD_LINE") {
		t.Fatalf("the formatted body never replaced the built-in one:\n%s", after)
	}
}

// Every marked shell diff row fits the transcript width: the connector the card
// swaps in for DiffText's two-space indent is three cells wider, and the body
// must be laid out that much narrower or the row wraps back to column 0.
func TestMarkedShellDiffRowsFitTheTranscriptWidth(t *testing.T) {
	m, _ := testModel(t)
	long := strings.Repeat("x", 300)
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n context " + long + "\n-" + long + "\n+" + long + "\n"
	apply(m, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Args: `{"command":"git diff"}`}})
	apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Output: diff, OutputDiff: true}})

	cw := m.contentWidth()
	for _, l := range rawRows(m) {
		if w := ansi.StringWidth(l); w > cw {
			t.Fatalf("a marked shell diff row is %d cells wide, want <= %d:\n%q", w, cw, ansi.Strip(l))
		}
	}
}

// A writer call's diff card fits the transcript width too.
func TestWriteCardRowsFitTheTranscriptWidth(t *testing.T) {
	m, _ := testModel(t)
	long := strings.Repeat("x", 300)
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n context " + long + "\n-" + long + "\n+" + long + "\n"
	apply(m, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "w1", Name: "edit_file", Args: `{"path":"x.go"}`}})
	apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "w1", Name: "edit_file", Args: `{"path":"x.go"}`, Diff: diff, Added: 1, Removed: 1}})

	cw := m.contentWidth()
	for _, l := range rawRows(m) {
		if w := ansi.StringWidth(l); w > cw {
			t.Fatalf("a write card row is %d cells wide, want <= %d:\n%q", w, cw, ansi.Strip(l))
		}
	}
}

// The rows a formatter produced fit the transcript width as well: the run's
// output is clamped to the body width the card actually shows.
func TestFormattedShellDiffRowsFitTheTranscriptWidth(t *testing.T) {
	landed := diffFormatHarness(t)
	m, _ := testModel(t)
	long := strings.Repeat("x", 300)
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n context " + long + "\n-" + long + "\n+" + long + "\n"
	apply(m, eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Args: `{"command":"git diff"}`}})
	apply(m, eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Output: diff, OutputDiff: true}})
	_ = cachedRows(m)
	key := waitLanded(t, landed)
	m.Update(diffFormattedMsg{key: key})

	cw := m.contentWidth()
	for _, l := range rawRows(m) {
		if w := ansi.StringWidth(l); w > cw {
			t.Fatalf("a formatted shell diff row is %d cells wide, want <= %d:\n%q", w, cw, ansi.Strip(l))
		}
	}
}

// A diff collapses to fewer rows when its formatter lands: the raw preamble it
// drew while the run was in flight becomes one formatted line. A collapse
// larger than the viewport must re-anchor the transcript to its tail — holding
// the old position would put every transcript row past the content, so the
// whole transcript would read as empty while the composer stayed.
func TestCollapsedDiffReanchorsTheTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable stdin→stdout filter")
	}
	restore := termrender.SetDiffFormatterForTest([]string{"head", "-n", "1"})
	landed := make(chan termrender.DiffKey, 8)
	termrender.SetDiffFormatNotify(func(k termrender.DiffKey) { landed <- k })
	t.Cleanup(func() {
		termrender.SetDiffFormatNotify(nil)
		restore()
	})

	m, _ := testModel(t)
	fillTranscript(m, 40) // taller than the viewport
	m.View()

	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,40 +1,40 @@\n" + strings.Repeat("-old\n+new\n", 20)
	apply(m,
		eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Args: `{"command":"git diff"}`}},
		eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Output: diff, OutputDiff: true}},
	)
	m.View()
	key := waitLanded(t, landed)
	m.Update(diffFormattedMsg{key: key})
	m.View()

	if total := len(m.content(m.liveLines())); m.scr.yoff >= total {
		t.Fatalf("the transcript blanked after the diff collapsed: yoff=%d total=%d", m.scr.yoff, total)
	}
}

// A transcript holding more diff keys than the memo must still settle: each
// landed run is fed back the way the program feeds it, and a run may only be
// answered by repainting the block that asked for its key, so a repaint cannot
// spawn runs for blocks it never touched. Keyless, every landed run dropped
// every diff block and a transcript past the memo size never converged.
func TestManyDiffBlocksSettleRatherThanLoop(t *testing.T) {
	const n = 160 // more diff keys than diffFormatCacheMax
	landed := diffFormatHarness(t)
	m, _ := testModel(t)
	for i := range n {
		id := fmt.Sprintf("t%d", i)
		diff := fmt.Sprintf("diff --git a/f%d.go b/f%d.go\n--- a/f%d.go\n+++ b/f%d.go\n@@ -1 +1 @@\n-old%d\n+new%d\n", i, i, i, i, i, i)
		apply(m,
			eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: id, Name: "bash", Args: `{"command":"git diff"}`}},
			eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: id, Name: "bash", Output: diff, OutputDiff: true}},
		)
	}
	_ = cachedRows(m) // first render: every distinct block starts one run

	answered := 0
	for {
		select {
		case k := <-landed:
			m.Update(diffFormattedMsg{key: k})
			_ = cachedRows(m)
			answered++
			if answered > 4*n {
				t.Fatalf("the landed-run repaint keeps spawning runs (%d answered)", answered)
			}
		case <-time.After(time.Second):
			return // quiesced: no run landed for a second
		}
	}
}
