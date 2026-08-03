package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/provider"
)

// TestTranscriptBlocksWidthWithSidebar pins the chat-column width contract:
// every block committed while the todo sidebar is pinned — user bubble,
// streamed/finalized assistant markdown, reasoning — must render to the chat
// column width (contentWidth minus the scrollbar column), never the full
// terminal width, or the block would spill into the sidebar column.
func TestTranscriptBlocksWidthWithSidebar(t *testing.T) {
	m := menuMouseFixture(t, 0, 160, 30, true)
	want := m.contentWidth() - 1 // scrollbar column

	// User bubble committed while the sidebar is active: the frame must fill
	// the chat column exactly (border rails span width-1).
	m.commitTranscriptSource(transcriptSource{kind: transcriptSourceUser, raw: "hello", planMode: false, ts: ""})
	block := m.transcript[len(m.transcript)-1]
	for i, l := range strings.Split(block, "\n") {
		if w := ansi.StringWidth(l); w != want {
			t.Fatalf("user bubble row %d width = %d, want %d (chat column): %q", i, w, want, l)
		}
	}

	// Streamed answer block (setTranscriptBlock path).
	m.answerIdx = len(m.transcript)
	raw := "streamed answer " + strings.Repeat("word ", 20)
	m.setTranscriptBlock(m.answerIdx, m.renderTranscriptSource(
		transcriptSource{kind: transcriptSourceMarkdown, raw: raw}, m.contentWidth()),
		transcriptSource{kind: transcriptSourceMarkdown, raw: raw})
	checkBlockWidth(t, m, "streamed answer", want)

	// Finalized answer (commitPending path).
	m.pending.Reset()
	m.pending.WriteString("final answer " + strings.Repeat("word ", 20))
	m.commitPending()
	checkBlockWidth(t, m, "finalized answer", want)

	// Reasoning block.
	m.reasoningTextIdx = len(m.transcript)
	m.setTranscriptBlock(m.reasoningTextIdx, reasoningBlock("thinking", m.contentWidth(), 0),
		transcriptSource{kind: transcriptSourceReasoning, raw: "thinking", maxLines: 0})
	checkBlockWidth(t, m, "reasoning", want)

	// Tool card.
	m.commitTranscriptSource(transcriptSource{kind: transcriptSourceToolCard, raw: "bash", aux: `{"command":"go test"}`})
	checkBlockWidth(t, m, "tool card", want)

	// Replay bundle (resumed history) with a user message inside.
	m.commitTranscriptSource(transcriptSource{
		kind: transcriptSourceReplayBundle,
		history: []provider.Message{
			{Role: provider.RoleUser, Content: "Which version?"},
		},
	})
	checkBlockWidth(t, m, "replay bundle", want)
}

func checkBlockWidth(t *testing.T, m chatTUI, label string, want int) {
	t.Helper()
	if len(m.transcript) == 0 {
		t.Fatalf("%s: no transcript block", label)
	}
	block := m.transcript[len(m.transcript)-1]
	for i, l := range strings.Split(block, "\n") {
		if w := ansi.StringWidth(l); w > want {
			t.Fatalf("%s row %d width = %d, want <= %d (chat column): %q", label, i, w, want, l)
		}
	}
}
