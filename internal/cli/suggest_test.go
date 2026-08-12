package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSuggestionMsgSetsGhostWhenIdle(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiIdle

	out, _ := m.Update(suggestionMsg{text: "继续修复那个 bug"})
	m = out.(chatTUI)
	if m.ghostSuggestion != "继续修复那个 bug" {
		t.Fatalf("ghostSuggestion = %q, want %q", m.ghostSuggestion, "继续修复那个 bug")
	}
}

func TestSuggestionMsgIgnoredWhileRunning(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning

	out, _ := m.Update(suggestionMsg{text: "继续修复那个 bug"})
	m = out.(chatTUI)
	if m.ghostSuggestion != "" {
		t.Fatalf("ghostSuggestion = %q, want empty while running", m.ghostSuggestion)
	}
}

func TestSuggestionMsgIgnoredWhenInputNotEmpty(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiIdle
	m.input.SetValue("用户已开始输入")

	out, _ := m.Update(suggestionMsg{text: "继续修复那个 bug"})
	m = out.(chatTUI)
	if m.ghostSuggestion != "" {
		t.Fatalf("ghostSuggestion = %q, want empty when input is non-empty", m.ghostSuggestion)
	}
}

func TestTabAcceptsGhostSuggestion(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiIdle
	m.ghostSuggestion = "继续修复那个 bug"

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = out.(chatTUI)
	if m.ghostSuggestion != "" {
		t.Fatalf("ghostSuggestion = %q, want cleared after accept", m.ghostSuggestion)
	}
	if got := m.input.Value(); got != "继续修复那个 bug" {
		t.Fatalf("input = %q, want %q", got, "继续修复那个 bug")
	}
}

func TestTypingClearsGhostSuggestion(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiIdle
	m.ghostSuggestion = "继续修复那个 bug"

	out, _ := m.Update(tea.KeyPressMsg{Code: 'x'})
	m = out.(chatTUI)
	if m.ghostSuggestion != "" {
		t.Fatalf("ghostSuggestion = %q, want cleared on typing", m.ghostSuggestion)
	}
}

func TestRenderGhostSuggestion(t *testing.T) {
	m := newTestChatTUI()
	m.ghostSuggestion = "继续修复那个 bug"
	m.width = 80

	rendered := m.renderGhostSuggestion()
	if !strings.Contains(rendered, "继续修复那个 bug") {
		t.Fatalf("renderGhostSuggestion missing text: %q", rendered)
	}
	if !strings.Contains(rendered, "Tab to accept") {
		t.Fatalf("renderGhostSuggestion missing hint: %q", rendered)
	}
}

func TestRenderGhostSuggestionEmpty(t *testing.T) {
	m := newTestChatTUI()
	m.ghostSuggestion = ""
	if got := m.renderGhostSuggestion(); got != "" {
		t.Fatalf("renderGhostSuggestion = %q, want empty", got)
	}
}
