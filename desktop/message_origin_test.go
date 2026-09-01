package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestCurrentMessageOriginIsAuthoritativeAcrossDesktopHistory(t *testing.T) {
	quotedHostText := agent.CompletionValidationContinuationPrefix + " explain what this message means"
	msgs := []provider.Message{
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "wrapped", RawContent: quotedHostText},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: agent.MidTurnSteerPrefix + "\nuse the smaller patch", RawContent: "use the smaller patch"},
		{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "innocent looking continuation"},
	}

	if got := visibleHistoryUserTurns(msgs, identityPromptDisplay); got != 1 {
		t.Fatalf("visible user turns = %d, want only the explicitly user-authored turn", got)
	}
	turns := historyCheckpointTurns(msgs, identityPromptDisplay, map[int]int{0: 7, 2: 8, 3: 9})
	if len(turns) != 1 || turns[0] != 7 {
		t.Fatalf("checkpoint turns = %v, want [7]", turns)
	}
	history := historyMessages(msgs, identityPromptDisplay)
	if len(history) != 3 || history[0].Role != "user" || history[0].Content != quotedHostText ||
		history[2].Role != "notice" || history[2].Content != "↪ use the smaller patch" {
		t.Fatalf("history = %+v, want quoted host text plus one steer notice", history)
	}
}

func TestPromptHistoryUsesOriginAndRawContentForCurrentJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	quotedHostText := agent.CompletionValidationContinuationPrefix + " show this literally"
	raw := `{"role":"user","origin":"host","content":"ordinary looking host text"}` + "\n" +
		`{"role":"user","origin":"user","content":"wrapped provider text","raw_content":` + strconv.Quote(quotedHostText) + `}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != quotedHostText {
		t.Fatalf("prompt history = %+v, want the explicit user raw content only", entries)
	}
}
