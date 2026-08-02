package cli

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// TestScrollbarTrackAlwaysVisible pins the transcript scrollbar column as a
// permanent track: with nothing to scroll it still renders the rail, so the
// content width never shifts when the transcript crosses the one-screen
// boundary and the scrollbar never "disappears" at some terminal widths.
func TestScrollbarTrackAlwaysVisible(t *testing.T) {
	got := scrollbarCell(0, 5, 20, 0, 0) // total <= height: no thumb
	if ansi.Strip(got) != "│" {
		t.Fatalf("scrollbarCell with no overflow = %q, want the track rail", got)
	}
	// With overflow, the thumb and track rows render as before.
	if ansi.Strip(scrollbarCell(0, 30, 20, 0, 1)) != "█" {
		t.Fatalf("thumb row should render the thumb glyph")
	}
	if ansi.Strip(scrollbarCell(5, 30, 20, 0, 1)) != "│" {
		t.Fatalf("track row should render the rail glyph")
	}
}

// TestIncrementalWrapMatchesFullWrap proves the growth-only re-feed path
// (wrapFrom) produces exactly the same wrappedLines as a full re-wrap, so the
// incremental optimization never changes what the transcript renders.
func TestIncrementalWrapMatchesFullWrap(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)

	adv := func(msg tea.Msg) {
		n, _ := m.Update(msg)
		m = n.(chatTUI)
	}
	// Notice events commit transcript lines through the real event path.
	line := func(text string) tea.Msg {
		return agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
	}
	check := func(t *testing.T, phase string) {
		t.Helper()
		want := strings.Split(wrapTranscript(strings.Join(m.transcript, "\n"), m.viewport.Width()), "\n")
		if !reflect.DeepEqual(m.wrappedLines, want) {
			t.Fatalf("%s: incremental wrappedLines diverge from full wrap", phase)
		}
		// The cached joined text must stay in sync with the lines array.
		if got := m.wrappedText; got != strings.Join(m.wrappedLines, "\n") {
			t.Fatalf("%s: wrappedText cache out of sync with wrappedLines", phase)
		}
	}

	// Phase 1: a few blocks, including a line long enough to soft-wrap.
	adv(line("line one"))
	adv(line("this line is long enough that it must soft wrap across the transcript width boundary for sure"))
	adv(line("line three"))
	check(t, "phase 1")

	// Phase 2: growth only — the incremental path must append the same
	// wrapped rows a full re-wrap would produce.
	adv(line("fourth block"))
	adv(line("another block with a very long line that also wraps across the width of the transcript column"))
	check(t, "phase 2")

	// Phase 3: growth again, then an in-place rewrite (transcriptDirty) which
	// must force the full re-wrap path and stay consistent.
	adv(line("fifth block"))
	check(t, "phase 3")
	if len(m.wrappedLines) == 0 {
		t.Fatal("wrappedLines should not be empty")
	}
	idx := len(m.transcript) - 1
	m.transcript[idx] = "rewritten tail line"
	m.transcriptDirty = true
	adv(line("sixth block"))
	check(t, "phase 4 (in-place rewrite)")
}
