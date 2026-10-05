package sessionstore

import (
	"os"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// The checkpoint is what a reader falls back to when the log cannot answer, so
// the two must never describe different transcripts. An append the read model
// declines used to leave it on the previous save's history: a second lineage
// beside the log, waiting for the first fallback to pick it.
func TestDeclinedReadModelAppendStillLeavesOneLineage(t *testing.T) {
	dir := testenv.TempDir(t)
	path := dir + "/session.jsonl"
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	// The index a save is allowed to lose (it is warn-only there) is exactly
	// what the in-place append needs to prove the checkpoint is the log's
	// prefix, so losing it is how the decline happens in the field.
	if err := os.Remove(store.SessionDisplayIndex(path)); err != nil {
		t.Fatalf("drop display index: %v", err)
	}

	live.Add(provider.Message{Role: provider.RoleUser, Content: "第二句话", RawContent: "第二句话"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}

	logged, _, damaged, err := loadSessionMessages(path)
	if err != nil || damaged {
		t.Fatalf("replay = %v damaged=%v", err, damaged)
	}
	checkpoint, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if !messagesEqualForStorageList(logged, checkpoint) {
		t.Fatalf("checkpoint %q disagrees with the log %q",
			sessionUserTexts(checkpoint), sessionUserTexts(logged))
	}
	if got := sessionUserTexts(checkpoint); len(got) != 2 {
		t.Fatalf("checkpoint users = %q, want both turns", got)
	}
}

// Republishing the read model also republishes the index that proves it, so a
// session that lost one recovers the in-place append instead of paying a whole
// rewrite on every later save.
func TestRepublishedReadModelRearmsTheAppendPath(t *testing.T) {
	dir := testenv.TempDir(t)
	path := dir + "/session.jsonl"
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.SessionDisplayIndex(path)); err != nil {
		t.Fatal(err)
	}
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}

	idx, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil {
		t.Fatalf("index was not republished: %v", err)
	}
	if idx.MessageCount != 3 {
		t.Fatalf("index message count = %d, want 3", idx.MessageCount)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if idx.TranscriptSize != info.Size() {
		t.Fatalf("index describes %d bytes of a %d-byte checkpoint", idx.TranscriptSize, info.Size())
	}
}
