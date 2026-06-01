package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestTranscriptMirrorsCommits proves the alt-screen migration's foundation:
// every line commitLine sends to native scrollback is also captured in the
// transcript buffer (the future viewport's content source), in order.
func TestTranscriptMirrorsCommits(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compacted"})

	if len(m.transcript) != len(*m.pendingCommit) {
		t.Fatalf("transcript (%d) and pendingCommit (%d) should hold the same lines", len(m.transcript), len(*m.pendingCommit))
	}
	for i := range m.transcript {
		if m.transcript[i] != (*m.pendingCommit)[i] {
			t.Errorf("line %d mismatch: transcript=%q pendingCommit=%q", i, m.transcript[i], (*m.pendingCommit)[i])
		}
	}
}

// TestTranscriptViewportSizing proves the viewport tracks the terminal size and
// gets the rows left over after the pinned bottom region (input box + 2 status
// rows = 5 with an empty 1-line composer), and is fed the committed transcript.
func TestTranscriptViewportSizing(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)

	if got := m.bottomRows(); got != 5 {
		t.Fatalf("bottomRows with an empty composer = %d, want 5 (input 1 + border 2 + status 2)", got)
	}
	if m.viewport.Width() != 79 {
		t.Errorf("viewport content width = %d, want 79 (terminal 80 - 1 scrollbar column)", m.viewport.Width())
	}
	if want := m.transcriptHeight(); m.viewport.Height() != want || want != 19 {
		t.Errorf("viewport height = %d, transcriptHeight = %d, want 19 (24-5)", m.viewport.Height(), want)
	}
	if m.viewport.TotalLineCount() == 0 {
		t.Errorf("viewport should hold the committed banner after the first resize")
	}
}

// TestIngestEventRoutesByKind proves each event Kind lands in the right place:
// reasoning shows a collapsed live marker at once, while tool dispatch, blocked
// results, usage, notices, and coordinator phases each commit as their own
// scrollback line. Routing is by Kind, not by sniffing line prefixes.
func TestIngestEventRoutesByKind(t *testing.T) {
	// Reasoning shows a collapsed live marker at once, without the raw thinking.
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "weighing options"})
	if len(m.transcript) != 1 || !strings.Contains(m.transcript[0], "thinking") {
		t.Errorf("reasoning should show a live marker, transcript=%v", m.transcript)
	}
	if strings.Contains(m.transcript[0], "weighing options") {
		t.Errorf("reasoning text should stay hidden by default, transcript=%v", m.transcript)
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

func TestIngestEventShowsReasoningInVerboseMode(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "weighing options"})
	if !strings.Contains(m.reasoning.String(), "weighing options") {
		t.Errorf("verbose reasoning should buffer the text, got %q", m.reasoning.String())
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

	// The first real packet commits the bubble (blank + bubble) ahead of itself;
	// a reasoning packet then also shows its live thinking marker.
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "thinking…"})
	if m.bubblePending {
		t.Fatalf("first packet should commit the deferred bubble")
	}
	if n := len(*m.pendingCommit); n != 3 {
		t.Fatalf("expected blank + bubble + thinking marker, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[1], "hello world") {
		t.Errorf("committed bubble should carry the user text, got %q", (*m.pendingCommit)[1])
	}
	if !strings.Contains((*m.pendingCommit)[2], "thinking") {
		t.Errorf("reasoning packet should show the thinking marker, got %q", (*m.pendingCommit)[2])
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

// TestViewAltScreenFillsHeight proves the switch to alt-screen: View requests
// the alt buffer + mouse, and the frame is exactly the terminal height (the
// transcript viewport pads to fill above the pinned bottom region).
func TestViewAltScreenFillsHeight(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m0.(chatTUI).View()

	if !v.AltScreen {
		t.Error("View must request alt-screen so resize repaints the whole grid")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Error("View must enable mouse so the wheel scrolls the transcript")
	}
	if lines := strings.Count(v.Content, "\n") + 1; lines != 24 {
		t.Errorf("alt-screen frame = %d lines, want 24 (full terminal height)", lines)
	}
}

// TestTranscriptTailFollow proves the viewport pins to newest output while the
// user is at the bottom, and stops yanking once the user scrolls up.
func TestTranscriptTailFollow(t *testing.T) {
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})

	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	for i := 0; i < 12; i++ { // overflow the short viewport so there's room to scroll
		cur = adv(cur, notice)
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("new output while pinned should keep the viewport at the bottom")
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should break the bottom pin")
	}

	cur = adv(cur, notice)
	if cur.viewport.AtBottom() {
		t.Error("new output while scrolled up must preserve the reading position")
	}
}

func TestFoldedPasteUsesPlaceholderAndExpandsOnSend(t *testing.T) {
	m := newTestChatTUI()
	pasted := "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3,\n  \"d\": 4\n}"
	if !shouldFoldPastedText(pasted) {
		t.Fatal("five-line paste should fold")
	}

	m.insertFoldedPaste(pasted)
	display := m.input.Value()
	if display != "[Pasted text #1 · 6 lines] " {
		t.Fatalf("display = %q", display)
	}

	sent := m.expandPastedBlocks(display)
	for _, want := range []string{
		"--- Begin [Pasted text #1 · 6 lines] ---",
		`"d": 4`,
		"--- End [Pasted text #1 · 6 lines] ---",
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("expanded paste missing %q in:\n%s", want, sent)
		}
	}
}

func TestPasteMsgFoldsBeforeTextareaConsumesNewlines(t *testing.T) {
	m := newTestChatTUI()
	model, _ := m.Update(tea.PasteMsg{Content: "1\n2\n3\n4\n5"})
	got := model.(chatTUI)
	if got.input.Value() != "[Pasted text #1 · 5 lines] " {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.input.Height() != 1 {
		t.Fatalf("folded paste should keep one input row, got %d", got.input.Height())
	}
}

func TestUnsendRestoresFoldedPastePlaceholder(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.pendingBubble = "expanded JSON"
	m.pendingRestore = "[Pasted text #1 · 5 lines] 这是什么?"
	m.bubblePending = true
	m.state = tuiRunning

	m.unsendPending()

	if got := m.input.Value(); got != "[Pasted text #1 · 5 lines] 这是什么?" {
		t.Fatalf("restored input = %q", got)
	}
	if m.pendingBubble != "" || m.pendingRestore != "" || m.bubblePending {
		t.Fatalf("pending state not cleared: bubble=%q restore=%q pending=%v", m.pendingBubble, m.pendingRestore, m.bubblePending)
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
