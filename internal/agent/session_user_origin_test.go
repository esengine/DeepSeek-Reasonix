package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestLoadSessionUserMessagesPreservesOriginAndRawContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{
		Role: provider.RoleUser, Origin: provider.MessageOriginUser,
		Content: "wrapped provider prompt", RawContent: "exact prompt",
	})
	s.Add(HostGeneratedUserMessage("host continuation without a legacy prefix"))
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	users, err := LoadSessionUserMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || UserMessageText(users[0].Message) != "exact prompt" ||
		users[0].Message.Origin != provider.MessageOriginUser ||
		users[1].Message.Origin != provider.MessageOriginHost {
		t.Fatalf("loaded users = %+v, want complete origin-aware messages", users)
	}
}
