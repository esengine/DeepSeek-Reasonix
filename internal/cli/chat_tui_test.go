package cli

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"

	tea "charm.land/bubbletea/v2"
)

// TestIngestEventRoutesByKind proves each event Kind lands in the right place:
// reasoning accumulates in its live buffer (uncommitted), while tool dispatch,
// blocked results, usage, notices, and coordinator phases each commit as their
// own scrollback line. Routing is by Kind, not by sniffing line prefixes.
func TestIngestEventRoutesByKind(t *testing.T) {
	// Reasoning stays live (dim), not committed.
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "weighing options"})
	if len(*m.pendingCommit) != 0 {
		t.Errorf("reasoning should stay live, committed=%v", *m.pendingCommit)
	}
	if !strings.Contains(m.reasoning.String(), "weighing options") {
		t.Errorf("reasoning should buffer the text, got %q", m.reasoning.String())
	}

	for _, tc := range []struct {
		name string
		ev   event.Event
		want string
	}{
		{"dispatch", event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}}, "  -> read_file {\"path\":\"x\"}"},
		{"blocked", event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}}, "  ⊘ bash blocked by permission policy"},
		{"usage", event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CacheHitTokens: 900, CacheMissTokens: 100}}, "  · 1200 tok"},
		{"notice-info", event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compacted 8 messages → summary"}, "  · compacted 8 messages → summary"},
		{"notice-warn", event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "response truncated: hit max output tokens"}, "  ! response truncated: hit max output tokens"},
		{"phase", event.Event{Kind: event.Phase, Text: "planner · planning"}, "[planner · planning]"},
	} {
		m := newTestChatTUI()
		m.ingestEvent(tc.ev)
		got := *m.pendingCommit
		if len(got) != 1 || !strings.Contains(got[0], tc.want) {
			t.Errorf("%s: committed=%v, want a single line containing %q", tc.name, got, tc.want)
		}
	}

	// A successful tool result is silent — it only feeds the model.
	m = newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file", Output: "contents"}})
	if len(*m.pendingCommit) != 0 {
		t.Errorf("successful tool result should be silent, committed=%v", *m.pendingCommit)
	}
}

// TestDeferredUserBubble proves the user bubble is held back until the server's
// first real packet: a local TurnStarted must not commit it (that would shrink
// the un-send window to nothing), while the first Reasoning/Text/etc. flushes it
// — a blank separator then the bubble — just before rendering that packet.
func TestDeferredUserBubble(t *testing.T) {
	m := newTestChatTUI()
	// Stand in for startTurn's deferral (no controller in the unit harness).
	m.pendingBubble = "hello world"
	m.bubblePending = true
	m.state = tuiRunning

	// TurnStarted is emitted locally before the request — it must not flush.
	m.ingestEvent(event.Event{Kind: event.TurnStarted})
	if !m.bubblePending || len(*m.pendingCommit) != 0 {
		t.Fatalf("TurnStarted should not commit the deferred bubble, pending=%v committed=%v", m.bubblePending, *m.pendingCommit)
	}

	// The first real packet commits the bubble (blank + bubble) ahead of itself.
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "thinking…"})
	if m.bubblePending {
		t.Fatalf("first packet should commit the deferred bubble")
	}
	if n := len(*m.pendingCommit); n != 2 {
		t.Fatalf("expected a blank separator + the bubble, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[1], "hello world") {
		t.Errorf("committed bubble should carry the user text, got %q", (*m.pendingCommit)[1])
	}
}

// TestUnsendDiscardsBufferedEvents proves that after an un-send (Esc before any
// packet) the turn's already-buffered events are swallowed — nothing reaches
// scrollback — and its TurnDone settles the model back to idle.
func TestUnsendDiscardsBufferedEvents(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.turnDiscarded = true // the state unsendPending leaves behind

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "late thinking"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "late answer"})
	if len(*m.pendingCommit) != 0 || m.reasoning.Len() != 0 || m.pending.Len() != 0 {
		t.Fatalf("a discarded turn should swallow buffered events, committed=%v", *m.pendingCommit)
	}

	m.ingestEvent(event.Event{Kind: event.TurnDone})
	if m.turnDiscarded || m.state != tuiIdle {
		t.Fatalf("TurnDone should clear the discard and return to idle, discarded=%v state=%v", m.turnDiscarded, m.state)
	}
	if len(*m.pendingCommit) != 0 {
		t.Errorf("a discarded turn should leave nothing in scrollback, committed=%v", *m.pendingCommit)
	}
}

// TestAnswerTextStartingWithBracketStaysInAnswer locks in the win of the typed
// event stream: model answer text starting with "[" — a markdown link, a slice
// literal, even a quoted "[… · planning]" — is a Text event, so it can never be
// mistaken for a coordinator phase marker the way prefix-sniffing a flattened
// byte stream once could. It stays in the answer buffer and renders as markdown.
func TestAnswerTextStartingWithBracketStaysInAnswer(t *testing.T) {
	for _, txt := range []string{
		"[link](https://example.com)",
		"[1, 2, 3]",
		"[planner · planning] (the model quoting a marker)",
	} {
		m := newTestChatTUI()
		m.ingestEvent(event.Event{Kind: event.Text, Text: txt})
		if len(*m.pendingCommit) != 0 {
			t.Errorf("answer text %q should stay live, not commit as an event line: %v", txt, *m.pendingCommit)
		}
		if m.pending.String() != txt {
			t.Errorf("answer text should buffer verbatim, got %q want %q", m.pending.String(), txt)
		}
	}
}

// TestInsertNewlineKeyBinding verifies newChatTUI actually wires shift+enter
// into the textarea's InsertNewline binding (plain Enter submits, so a newline
// needs a modifier). It exercises the real constructor, not a hand-built binding.
func TestInsertNewlineKeyBinding(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	keys := m.input.KeyMap.InsertNewline.Keys()
	found := false
	for _, k := range keys {
		if k == "shift+enter" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("newChatTUI InsertNewline should include shift+enter, got %v", keys)
	}
}

func TestApprovalToolDetailsShortensMCPNames(t *testing.T) {
	name, detail := approvalToolDetails("mcp__minimax-coding-plan-mcp__understand_image")
	if name != "understand_image" {
		t.Fatalf("name = %q, want understand_image", name)
	}
	for _, want := range []string{"provided image input", "minimax-coding-plan-mcp"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail = %q, want it to contain %q", detail, want)
		}
	}

	name, detail = approvalToolDetails("bash")
	if name != "bash" || !strings.Contains(detail, "built-in") {
		t.Errorf("built-in details = (%q, %q), want bash + built-in source", name, detail)
	}
}

// TestDoubleCtrlCQuit verifies that Ctrl+C while idle requires a double-press
// within the 1.5s window to actually quit. A single press shows a hint; a
// second press within the window returns tea.Quit.
func TestDoubleCtrlCQuit(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4} // 4 = ModCtrl

	// First Ctrl+C while idle: arms quit, flushes hint via finalize cmd.
	out, cmd := m.Update(ctrlC)
	if cmd == nil {
		t.Error("first Ctrl+C should return a finalize cmd to flush the hint")
	}
	m2, ok := out.(chatTUI)
	if !ok {
		t.Fatalf("Update returned %T, want chatTUI", out)
	}
	if m2.lastCtrlCAt.IsZero() {
		t.Error("first Ctrl+C should set lastCtrlCAt")
	}

	// Second Ctrl+C within window: returns tea.Quit.
	out2, cmd2 := m2.Update(ctrlC)
	if cmd2 == nil {
		t.Error("second Ctrl+C within window should return a quit cmd")
	}
	_ = out2

	// Window expired: re-arms instead of quitting (still flushes hint via finalize).
	m3 := m2
	m3.lastCtrlCAt = time.Now().Add(-2 * time.Second)
	out4, cmd4 := m3.Update(ctrlC)
	if cmd4 == nil {
		t.Error("expired Ctrl+C should return a finalize cmd to flush the re-armed hint")
	}
	m4, ok := out4.(chatTUI)
	if !ok {
		t.Fatalf("Update returned %T, want chatTUI", out4)
	}
	// lastCtrlCAt should be refreshed to now.
	if time.Since(m4.lastCtrlCAt) > time.Second {
		t.Error("expired Ctrl+C should refresh lastCtrlCAt")
	}
}
