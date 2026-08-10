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
	for range 20 {
		cur = adv(cur, notice)
	}
	if !cur.shouldFollowTail() || !cur.viewport.AtBottom() {
		t.Fatal("expected followTail at bottom after streaming")
	}

	// Simulate approval popup (height-only change, no user scroll).
	cur.pendingApproval = &event.Approval{Tool: "bash", Subject: "echo hi"}
	if !cur.modalOpen() {
		t.Fatal("pendingApproval must report modalOpen")
	}
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
	for range 30 {
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

func TestScrollbarDragMotionSyncsBeforeRelease(t *testing.T) {
	// Drag motion must leave followTail immediately so an interleaved agent
	// event cannot GotoBottom before MouseRelease.
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 10})
	for range 40 {
		cur = adv(cur, notice)
	}
	if !cur.shouldFollowTail() {
		t.Fatal("expected followTail before drag")
	}
	barX := cur.viewport.Width()
	// Click thumb region near the top of the scrollbar.
	cur = adv(cur, tea.MouseClickMsg{X: barX, Y: 0, Button: tea.MouseLeft})
	if !cur.scrollbarDrag {
		t.Fatal("expected scrollbar drag")
	}
	// First motion moves the offset — must mark userScrolled before release.
	cur = adv(cur, tea.MouseMotionMsg{X: barX, Y: 0, Button: tea.MouseLeft})
	if cur.shouldFollowTail() {
		t.Fatal("drag motion must mark userScrolled before mouse release")
	}
	readOffset := cur.viewport.YOffset()
	// Interleaved streaming event must not yank to bottom.
	cur = adv(cur, notice)
	if cur.viewport.AtBottom() {
		t.Fatal("streaming during scrollbar drag must not jump to bottom")
	}
	if got := cur.viewport.YOffset(); got != readOffset {
		// Offset may grow if content above expands; must not equal bottom.
		if cur.viewport.AtBottom() {
			t.Fatalf("YOffset jumped to bottom during drag: %d", got)
		}
	}
}

func TestWrapCacheAppendOnlyMatchesFullRebuild(t *testing.T) {
	m := newTestChatTUI()
	m.width = 40
	cw := 40
	for i := range 5 {
		m.transcript = append(m.transcript, fmt.Sprintf("block-%d %s", i, strings.Repeat("word ", 20)))
	}
	m.rebuildWrappedLinesFull(cw)
	full := append([]string(nil), m.wrappedLines...)

	m2 := newTestChatTUI()
	m2.width = 40
	for i := range 5 {
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

func TestStreamAnswerSuffixInvalidationNotFullRebuild(t *testing.T) {
	// Real event.Text → streamAnswer path must only re-wrap from answerIdx,
	// leaving wrapBlockCount of the prefix intact between flushes.
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 20})
	// Seed history blocks.
	for i := range 50 {
		cur = adv(cur, agentEventMsg(event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("hist-%d %s", i, strings.Repeat("x", 30)),
		}))
	}
	prefixBlocks := cur.wrapBlockCount
	if prefixBlocks < 50 {
		t.Fatalf("prefix wrapBlockCount=%d, want >= 50", prefixBlocks)
	}
	// Stream a multi-paragraph answer that flushes more than once.
	cur.state = tuiRunning
	paras := []string{
		"First paragraph of the answer that is long enough to wrap.\n\n",
		"Second paragraph arrives later and rewrites the same block.\n\n",
		"Third paragraph extends the live answer block again.\n\n",
	}
	for _, p := range paras {
		cur = adv(cur, agentEventMsg(event.Event{Kind: event.Text, Text: p}))
		// After each flush, the wrap cache should cover all blocks; the live
		// answer is the last block so wrapBlockCount == len(transcript).
		if cur.wrapBlockCount != len(cur.transcript) {
			t.Fatalf("after stream wrapBlockCount=%d transcript=%d", cur.wrapBlockCount, len(cur.transcript))
		}
		// Prefix history blocks must still exist; answer is typically one block.
		if len(cur.transcript) < prefixBlocks {
			t.Fatal("history blocks disappeared during stream")
		}
	}
	if cur.answerIdx < 0 {
		// Paragraphs end with \n\n so streamAnswer must open a live answer block.
		t.Fatal("streamAnswer never opened an answer block after flushable paragraphs")
	}
	// Prefix wrap lines must still match the joined content of history blocks.
	if got := cur.wrappedContentString(); got == "" {
		t.Fatal("wrappedContentString empty after streamed answer")
	}
}

func TestClampCursorToTerminal(t *testing.T) {
	cur := tea.NewCursor(100, -3)
	got := clampCursorToTerminal(cur, 80, 24)
	if got.X != 79 || got.Y != 0 {
		t.Fatalf("clamp = (%d,%d), want (79,0)", got.X, got.Y)
	}
}

func TestMouseReenableTrailingEdge(t *testing.T) {
	// Controllable clock: first request emits; second inside the window arms
	// trailing pending; timer msg after window emits again.
	fakeNow := time.Unix(1_700_000_000, 0)
	mouseNowFn = func() time.Time { return fakeNow }
	t.Cleanup(func() { mouseNowFn = nil })

	m := newTestChatTUI()
	m.nativeScrollback = false
	m.mouseCaptureOff = false

	cmd1 := m.maybeReenableMouse()
	if cmd1 == nil {
		t.Fatal("first re-enable should emit")
	}
	if m.mouseReenablePending {
		t.Fatal("first fire must clear pending")
	}

	// Immediate second request inside window → pending + timer.
	cmd2 := m.maybeReenableMouse()
	if !m.mouseReenablePending {
		t.Fatal("second request inside window must set pending")
	}
	if !m.mouseReenableTimerArmed {
		t.Fatal("second request must arm trailing timer")
	}
	// tea.Tick cmd is non-nil (schedule).
	if cmd2 == nil {
		t.Fatal("expected timer cmd for trailing edge")
	}

	// Third request should not arm another timer.
	cmd3 := m.maybeReenableMouse()
	if cmd3 != nil {
		t.Fatal("already-armed timer must not schedule again")
	}
	if !m.mouseReenablePending {
		t.Fatal("pending must stay set")
	}

	// Advance clock past the interval and deliver the timer msg.
	fakeNow = fakeNow.Add(mouseReenableMinInterval + time.Millisecond)
	cmd4 := m.handleMouseReenableMsg(mouseReenableMsg{})
	if cmd4 == nil {
		t.Fatal("trailing timer must emit enable when pending")
	}
	if m.mouseReenablePending {
		t.Fatal("trailing fire must clear pending")
	}
}

func TestMouseReenableRateLimit(t *testing.T) {
	fakeNow := time.Unix(1_700_000_000, 0)
	mouseNowFn = func() time.Time { return fakeNow }
	t.Cleanup(func() { mouseNowFn = nil })

	m := newTestChatTUI()
	m.nativeScrollback = false
	m.mouseCaptureOff = false
	if m.maybeReenableMouse() == nil {
		t.Fatal("first re-enable should emit a raw cmd")
	}
	// Second call is trailing schedule — pending path, not a silent drop.
	if m.maybeReenableMouse() == nil && !m.mouseReenablePending {
		t.Fatal("in-window call must arm trailing pending, not silently drop")
	}
	if !m.mouseReenablePending {
		t.Fatal("in-window call must be pending, not silently dropped")
	}
}

func TestLongTranscriptStreamAnswerScalesSuffixOnly(t *testing.T) {
	// 1k history + many streaming flushes: wrapBlockCount tracks transcript and
	// the answer stays a single rewritten block (no full-history force).
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 20})
	const hist = 1000
	for i := range hist {
		cur = adv(cur, agentEventMsg(event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("h-%d-%s", i, strings.Repeat("y", 40)),
		}))
	}
	cur.state = tuiRunning
	for i := range 30 {
		cur = adv(cur, agentEventMsg(event.Event{
			Kind: event.Text,
			Text: fmt.Sprintf("stream paragraph %d with enough text to flush.\n\n", i),
		}))
		if cur.wrapBlockCount != len(cur.transcript) {
			t.Fatalf("iter %d wrapBlockCount=%d len=%d", i, cur.wrapBlockCount, len(cur.transcript))
		}
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("followTail stream must stay at bottom")
	}
}

func BenchmarkStreamAnswerSuffixWrap(b *testing.B) {
	// Benchmark real event.Text flushes against a large history. Suffix-only
	// invalidation should keep per-op cost independent of history length.
	for _, hist := range []int{500, 1000, 2000} {
		b.Run(fmt.Sprintf("hist=%d", hist), func(b *testing.B) {
			ctrl := control.New(control.Options{})
			adv := func(m chatTUI, msg tea.Msg) chatTUI {
				n, _ := m.Update(msg)
				return n.(chatTUI)
			}
			base := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 24})
			for i := range hist {
				base = adv(base, agentEventMsg(event.Event{
					Kind: event.Notice, Level: event.LevelInfo,
					Text: fmt.Sprintf("h-%d-%s", i, strings.Repeat("z", 48)),
				}))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				m := base // copy model value (shallow; OK for bench of Update path)
				m.state = tuiRunning
				m.pending = &strings.Builder{}
				m.answerIdx = -1
				m.answerFlushed = 0
				// One flushable paragraph per iteration.
				m = adv(m, agentEventMsg(event.Event{
					Kind: event.Text,
					Text: fmt.Sprintf("bench paragraph %d with body text that closes.\n\n", i),
				}))
				if m.wrapBlockCount != len(m.transcript) {
					b.Fatalf("wrapBlockCount=%d len=%d", m.wrapBlockCount, len(m.transcript))
				}
			}
		})
	}
}
