package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestRenameSessionUpdatesCustomTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "test-session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		TopicTitle: "Topic",
		CreatedAt:  updatedAt.Add(-time.Hour),
		UpdatedAt:  updatedAt,
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
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
		TopicTitle  string `json:"topic_title"`
		CustomTitle string `json:"custom_title"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding meta: %v", err)
	}
	if m.CustomTitle != "My Test Title" {
		t.Errorf("custom_title = %q, want %q", m.CustomTitle, "My Test Title")
	}
	if m.TopicTitle != "Topic" {
		t.Errorf("topic_title = %q, want preserved Topic", m.TopicTitle)
	}
	stored, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after rename ok=%v err=%v", ok, err)
	}
	if !stored.UpdatedAt.Equal(updatedAt) {
		t.Errorf("updated_at changed after rename: got %s want %s", stored.UpdatedAt, updatedAt)
	}
	if err := agent.RenameSession(sessionPath, "Updated Title"); err != nil {
		t.Fatalf("second rename failed: %v", err)
	}
	raw, _ = os.ReadFile(metaPath)
	json.Unmarshal(raw, &m)
	if m.CustomTitle != "Updated Title" {
		t.Errorf("custom_title after second rename = %q, want %q", m.CustomTitle, "Updated Title")
	}
	if m.TopicTitle != "Topic" {
		t.Errorf("topic_title after second rename = %q, want preserved Topic", m.TopicTitle)
	}
}

func TestSessionPickerLabelPrefersCustomTitle(t *testing.T) {
	s := agent.SessionInfo{Turns: 5, Preview: "first user message here", TopicTitle: ""}
	got := sessionPickerLabel(s)
	if got == "" {
		t.Fatal("empty label")
	}
	s.TopicTitle = "My Topic Name"
	s.CustomTitle = "My Custom Name"
	got = sessionPickerLabel(s)
	if !strings.Contains(got, "My Custom Name") {
		t.Errorf("label %q should contain custom title", got)
	}
	if strings.Contains(got, "My Topic Name") {
		t.Errorf("label %q should prefer custom title over topic title", got)
	}
}
