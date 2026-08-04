package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Older builds persisted display fields polluted with internal wrappers.
// LoadBranchMeta is the single read boundary, so it must return the
// user-authored text instead (#5666).
func TestLoadBranchMetaSanitizesPollutedDisplayFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	polluted := realContract("fix the login bug")
	if err := SaveBranchMeta(path, BranchMeta{
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		TopicTitle:  polluted,
		CustomTitle: polluted,
		Preview:     polluted,
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	for name, got := range map[string]string{
		"TopicTitle":  meta.TopicTitle,
		"CustomTitle": meta.CustomTitle,
		"Preview":     meta.Preview,
	} {
		if got != "fix the login bug" {
			t.Errorf("%s = %q, want the unwrapped user prompt", name, got)
		}
	}

	// Clean values pass through untouched.
	if err := SaveBranchMeta(path, BranchMeta{CreatedAt: time.Now(), UpdatedAt: time.Now(), TopicTitle: "My topic"}); err != nil {
		t.Fatalf("SaveBranchMeta clean: %v", err)
	}
	meta, _, err = LoadBranchMeta(path)
	if err != nil {
		t.Fatalf("LoadBranchMeta clean: %v", err)
	}
	if meta.TopicTitle != "My topic" {
		t.Errorf("clean TopicTitle = %q, want unchanged", meta.TopicTitle)
	}
}
