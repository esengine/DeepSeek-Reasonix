package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestSaveLoadPreservesMessageOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := NewSession("system")
	session.Add(provider.Message{
		Role: provider.RoleUser, Origin: provider.MessageOriginUser,
		Content: "wrapped input", RawContent: "user input",
	})
	session.Add(HostGeneratedUserMessage("host continuation"))
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := loaded.Snapshot()
	if messages[1].Origin != provider.MessageOriginUser || messages[2].Origin != provider.MessageOriginHost {
		t.Fatalf("reloaded origins = %q/%q, want user/host", messages[1].Origin, messages[2].Origin)
	}
}
