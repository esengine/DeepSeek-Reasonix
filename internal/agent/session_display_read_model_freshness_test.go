package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestAppendSessionDisplayReadModelRejectsEqualMTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "equal-mtime.jsonl")
	base := []provider.Message{
		{Role: provider.RoleUser, Content: "question"},
		{Role: provider.RoleAssistant, Content: "answer"},
	}
	session := NewSession("sys")
	for _, message := range base {
		session.Add(message)
	}
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	full := append(append([]provider.Message(nil), base...), provider.Message{Role: provider.RoleUser, Content: "new turn"})
	digest, err := digestSessionMessages(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendSessionReplaceEvent(path, full, digest, meta.Revision, "equal-mtime-test"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	equalTime := time.Now().Add(-time.Second)
	for _, artifact := range []string{path, store.SessionDisplayIndex(path)} {
		if err := os.Chtimes(artifact, equalTime, equalTime); err != nil {
			t.Fatal(err)
		}
	}

	appended, err := appendSessionDisplayReadModel(path, full, len(base), meta.Revision)
	if err != nil || appended {
		t.Fatalf("equal-mtime append = %v, err=%v; want stale fallback", appended, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("stale incremental path changed the compatibility transcript")
	}
	if err := RepairSessionDisplayReadModel(path); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil || idx.MessageCount != len(full) {
		t.Fatalf("replayed display index = %+v err=%v", idx, err)
	}
}
