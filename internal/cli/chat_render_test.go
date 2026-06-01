package cli

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"reasonix/internal/event"
)

// newTestChatTUI builds a chatTUI with just the pieces the streaming/commit and
// completion paths need, for unit tests that don't run the bubbletea loop.
func newTestChatTUI() chatTUI {
	commit := []string{}
	ti := textarea.New()
	ti.SetWidth(80)
	return chatTUI{
		input:         ti,
		nextPasteID:   1,
		reasoning:     &strings.Builder{},
		pending:       &strings.Builder{},
		pendingCommit: &commit,
		renderer:      newMarkdownRenderer(80),
	}
}

// TestIngestSeparatesReasoningFromAnswer proves the print-above model commits
// the collapsed reasoning marker and the answer as distinct scrollback entries
// (joined with a newline downstream), so the answer never butts up against the
// thinking marker — and that the answer stays live until it's flushed.
func TestIngestSeparatesReasoningFromAnswer(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "…reasoning…"}) // header + fragment → live buffer
	if len(*m.pendingCommit) != 0 {
		t.Fatalf("reasoning should stay live until the answer begins, committed=%v", *m.pendingCommit)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "Hello answer"}) // answer begins → reasoning finalizes
	if n := len(*m.pendingCommit); n != 1 || !strings.Contains((*m.pendingCommit)[0], "thinking") {
		t.Fatalf("reasoning should commit when the answer begins, committed=%v", *m.pendingCommit)
	}
	if strings.Contains((*m.pendingCommit)[0], "…reasoning…") {
		t.Fatalf("reasoning text should stay collapsed by default, committed=%v", *m.pendingCommit)
	}
	if m.pending.String() != "Hello answer" {
		t.Errorf("answer should be live in pending, got %q", m.pending.String())
	}
	if m.reasoning.Len() != 0 {
		t.Errorf("reasoning buffer should be cleared after commit")
	}

	m.commitPending() // turn end
	if n := len(*m.pendingCommit); n != 2 || !strings.Contains((*m.pendingCommit)[1], "Hello") {
		t.Fatalf("answer should commit on flush as a separate entry, committed=%v", *m.pendingCommit)
	}
}

// TestIngestEventFlushesAnswer confirms an event line (e.g. a tool dispatch)
// finalizes the answer streamed before it, preserving order in scrollback.
func TestIngestEventFlushesAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "partial answer "})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	if n := len(*m.pendingCommit); n != 2 {
		t.Fatalf("answer then event line should be two commits, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[0], "partial answer") {
		t.Errorf("first commit should be the buffered answer, got %q", (*m.pendingCommit)[0])
	}
	if !strings.Contains((*m.pendingCommit)[1], "-> read_file") {
		t.Errorf("second commit should be the event line, got %q", (*m.pendingCommit)[1])
	}
	if m.pending.Len() != 0 {
		t.Errorf("answer buffer should be drained after the event line")
	}
}
