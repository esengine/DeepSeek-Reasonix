package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestRenderGhostSuggestionInline(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("")
	m.ghostSuggestion = "继续修复那个 bug"

	rendered := m.renderComposerInput()
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "继续修复那个 bug") {
		t.Fatalf("inline ghost missing text: %q", plain)
	}
	// Ghost must render inline after the prompt glyph, not on its own row.
	if strings.Contains(plain, "\n继续修复那个 bug") {
		t.Fatalf("ghost rendered on its own row instead of inline: %q", plain)
	}
}

func TestRenderGhostSuggestionNotWhenInputNotEmpty(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("用户已输入")
	m.ghostSuggestion = "继续修复那个 bug"

	rendered := m.renderComposerInput()
	plain := ansi.Strip(rendered)
	if strings.Contains(plain, "继续修复那个 bug") {
		t.Fatalf("ghost leaked into non-empty input: %q", plain)
	}
}

func TestRenderGhostSuggestionEmpty(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("")
	m.ghostSuggestion = ""

	rendered := m.renderComposerInput()
	plain := ansi.Strip(rendered)
	if strings.Contains(plain, "继续修复那个 bug") {
		t.Fatalf("unexpected ghost text: %q", plain)
	}
}
