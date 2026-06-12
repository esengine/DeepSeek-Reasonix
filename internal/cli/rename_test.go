package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
)

func TestRenameSessionUpdatesTopicTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "test-session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.RenameSession(sessionPath, "My Test Title"); err != nil {
		t.Fatalf("RenameSession failed: %v", err)
	}
	metaPath := sessionPath + ".meta"
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading meta: %v", err)
	}
	var m struct {
		TopicTitle string `json:"topic_title"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding meta: %v", err)
	}
	if m.TopicTitle != "My Test Title" {
		t.Errorf("topic_title = %q, want %q", m.TopicTitle, "My Test Title")
	}
	if err := agent.RenameSession(sessionPath, "Updated Title"); err != nil {
		t.Fatalf("second rename failed: %v", err)
	}
	raw, _ = os.ReadFile(metaPath)
	json.Unmarshal(raw, &m)
	if m.TopicTitle != "Updated Title" {
		t.Errorf("topic_title after second rename = %q, want %q", m.TopicTitle, "Updated Title")
	}
}

func TestSessionPickerLabelShowsTopicTitle(t *testing.T) {
	s := agent.SessionInfo{Turns: 5, Preview: "first user message here", TopicTitle: ""}
	got := sessionPickerLabel(s)
	if got == "" {
		t.Fatal("empty label")
	}
	s.TopicTitle = "My Custom Name"
	got = sessionPickerLabel(s)
	if got == "" {
		t.Fatal("empty label after setting TopicTitle")
	}
	found := false
	for _, ch := range got {
		if ch == 'M' {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("label %q should contain topic title", got)
	}
}
