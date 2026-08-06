package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestModalOpenDoesNotDisableTailFollow(t *testing.T) {
	// Opening an approval banner shrinks the transcript viewport. Without an
	// explicit scroll state machine that used to make AtBottom() flip false and
	// permanently stop tail-follow (#6430).
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})

	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 12})
	for i := 0; i < 20; i++ {
		cur = adv(cur, notice)
	}
	if !cur.shouldFollowTail() || !cur.viewport.AtBottom() {
		t.Fatal("expected followTail at bottom after streaming")
	}

	// Simulate approval popup (height-only change, no user scroll).
	cur.pendingApproval = &event.Approval{Tool: "bash", Subject: "echo hi"}
	cur = adv(cur, tea.WindowSizeMsg{Width: 80, Height: 12})
	if !cur.shouldFollowTail() {
		t.Fatal("modal open must not mark userScrolled")
	}

	cur = adv(cur, notice)
	if !cur.viewport.AtBottom() {
		t.Fatalf("new output while modal open must keep followTail pin, YOffset=%d", cur.viewport.YOffset())
	}
}

func TestUserScrollBreaksAndEmptyEnterRestoresFollow(t *testing.T) {
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})

	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 30; i++ {
		cur = adv(cur, notice)
	}
	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.shouldFollowTail() {
		t.Fatal("wheel-up must mark userScrolled")
	}
	cur = adv(cur, notice)
	if cur.viewport.AtBottom() {
		t.Fatal("output while userScrolled must not yank to bottom")
	}
	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !cur.shouldFollowTail() || !cur.viewport.AtBottom() {
		t.Fatal("empty Enter must restore followTail and pin bottom")
	}
}

func TestWrapCacheAppendOnlyMatchesFullRebuild(t *testing.T) {
	m := newTestChatTUI()
	m.width = 40
	cw := 40
	// Seed several blocks via full rebuild.
	for i := 0; i < 5; i++ {
		m.transcript = append(m.transcript, fmt.Sprintf("block-%d %s", i, strings.Repeat("word ", 20)))
	}
	m.rebuildWrappedLinesFull(cw)
	full := append([]string(nil), m.wrappedLines...)

	// Reset and append one-by-one.
	m2 := newTestChatTUI()
	m2.width = 40
	for i := 0; i < 5; i++ {
		m2.transcript = append(m2.transcript, m.transcript[i])
		m2.syncWrappedLines(cw, i == 0)
	}
	if len(m2.wrappedLines) != len(full) {
		t.Fatalf("incremental lines=%d full=%d", len(m2.wrappedLines), len(full))
	}
	for i := range full {
		if m2.wrappedLines[i] != full[i] {
			t.Fatalf("line %d mismatch\ninc=%q\nfull=%q", i, m2.wrappedLines[i], full[i])
		}
	}
}

func TestClampCursorToTerminal(t *testing.T) {
	cur := tea.NewCursor(100, -3)
	got := clampCursorToTerminal(cur, 80, 24)
	if got.X != 79 || got.Y != 0 {
		t.Fatalf("clamp = (%d,%d), want (79,0)", got.X, got.Y)
	}
}

func TestMouseReenableRateLimit(t *testing.T) {
	m := newTestChatTUI()
	m.nativeScrollback = false
	m.mouseCaptureOff = false
	cmd1 := m.maybeReenableMouse()
	if cmd1 == nil {
		t.Fatal("first re-enable should emit a raw cmd")
	}
	cmd2 := m.maybeReenableMouse()
	if cmd2 != nil {
		t.Fatal("second re-enable within interval must be rate-limited")
	}
	m.lastMouseReenable = time.Now().Add(-2 * mouseReenableMinInterval)
	cmd3 := m.maybeReenableMouse()
	if cmd3 == nil {
		t.Fatal("re-enable after interval should emit again")
	}
}

func TestLongTranscriptAppendKeepsFollowAndDoesNotFullRebuildEachTime(t *testing.T) {
	// Smoke: 2k blocks streaming while followTail stays pinned; wrapBlockCount
	// tracks growth (append path) rather than staying at 0 after first frame.
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 20})
	const n = 2000
	for i := 0; i < n; i++ {
		cur = adv(cur, agentEventMsg(event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("line-%d-%s", i, strings.Repeat("x", 40)),
		}))
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("long stream while followTail must stay at bottom")
	}
	if cur.wrapBlockCount != len(cur.transcript) {
		t.Fatalf("wrapBlockCount=%d transcript=%d", cur.wrapBlockCount, len(cur.transcript))
	}
	if cur.wrapWidth <= 0 || len(cur.wrappedLines) == 0 {
		t.Fatal("expected populated wrap cache after long stream")
	}
}
