package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestAutoFollowStreamingAnswerAtBottom(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	// Start with a small terminal. WindowSizeMsg also commits the TUI banner.
	cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 10})

	// Add content to overflow the viewport (banner + 40 notice lines).
	for i := 0; i < 40; i++ {
		cur = adv(cur, agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"}))
	}

	// Should be at bottom after filling content
	if !cur.viewport.AtBottom() {
		t.Fatalf("expected at bottom after filling content, YOffset=%d", cur.viewport.YOffset())
	}

	// Now simulate streaming text while at bottom
	cur = adv(cur, agentEventMsg(event.Event{Kind: event.Text, Text: "This is a streamed response paragraph.\n\n"}))

	// Should still be at bottom after streaming new content
	if !cur.viewport.AtBottom() {
		t.Fatalf("expected at bottom after streaming text at bottom, YOffset=%d", cur.viewport.YOffset())
	}

	// More streaming content
	cur = adv(cur, agentEventMsg(event.Event{Kind: event.Text, Text: "More streamed content with wrapping.\n\n"}))

	if !cur.viewport.AtBottom() {
		t.Fatalf("expected at bottom after more streaming, YOffset=%d", cur.viewport.YOffset())
	}
}
