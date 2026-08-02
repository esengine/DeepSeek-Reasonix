package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestCloneSessionIncludesAuthoritativeEventLog builds a real session where
// the checkpoint lags behind the event log: the clone must carry the turns
// that only exist in the event log.
func TestCloneSessionIncludesAuthoritativeEventLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(src, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := []provider.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}
	s := &Session{Messages: msgs}
	if err := s.Save(src); err != nil {
		t.Fatal(err)
	}
	sourceMeta, ok, err := LoadBranchMeta(src)
	if err != nil || !ok {
		t.Fatalf("load source metadata: ok=%v err=%v", ok, err)
	}
	sourceMeta.Scope = "project"
	sourceMeta.WorkspaceRoot = dir
	sourceMeta.TopicID = "topic-copy"
	sourceMeta.TopicTitle = "Copy topic"
	sourceMeta.Model = "deepseek/model"
	if err := SaveBranchMetaPreserveUpdated(src, sourceMeta); err != nil {
		t.Fatal(err)
	}
	// A newer turn lands only in the event log: the checkpoint (jsonl) stays
	// behind because the log is authoritative once present.
	withThird := append(append([]provider.Message(nil), msgs...), provider.Message{Role: "assistant", Content: "third"})
	digest, _, err := digestAndSizeSessionMessages(withThird)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendSessionReplaceEvent(src, withThird, digest, 0, "test"); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "copy.jsonl")
	clone, err := CloneSessionToPath(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	lease := clone.Commit()
	if lease == nil {
		t.Fatal("clone commit did not transfer the destination lease")
	}
	defer lease.Release()
	cloned, err := LoadSession(dst)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range cloned.Messages {
		texts = append(texts, m.Content)
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(joined, want) {
			t.Errorf("clone missing %q: %v", want, texts)
		}
	}
	if _, err := os.Stat(SessionEventLogPath(dst)); err != nil {
		t.Errorf("clone has no event log: %v", err)
	}
	if _, err := os.Stat(BranchMetaPath(dst)); err != nil {
		t.Errorf("clone has no branch metadata: %v", err)
	}
	clonedMeta, ok, err := LoadBranchMeta(dst)
	if err != nil || !ok {
		t.Fatalf("load cloned metadata: ok=%v err=%v", ok, err)
	}
	if clonedMeta.ID != BranchID(dst) || clonedMeta.ID == sourceMeta.ID {
		t.Errorf("clone metadata ID = %q, want fresh ID for %q", clonedMeta.ID, dst)
	}
	if clonedMeta.Scope != sourceMeta.Scope || clonedMeta.WorkspaceRoot != sourceMeta.WorkspaceRoot ||
		clonedMeta.TopicID != sourceMeta.TopicID || clonedMeta.TopicTitle != sourceMeta.TopicTitle || clonedMeta.Model != sourceMeta.Model {
		t.Errorf("clone metadata binding/profile drifted: got %+v source %+v", clonedMeta, sourceMeta)
	}
	if clonedMeta.Revision <= 0 || clonedMeta.ContentDigest == "" {
		t.Errorf("clone metadata did not start its own persistence ledger: %+v", clonedMeta)
	}
}

func TestCloneSessionFailureCleansUpPartialCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(src, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Session{Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	if err := s.Save(src); err != nil {
		t.Fatal(err)
	}
	// An existing destination must be refused (O_EXCL) without touching it.
	dst := filepath.Join(dir, "existing.jsonl")
	if err := os.WriteFile(dst, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CloneSessionToPath(src, dst); err == nil {
		t.Fatal("clone over an existing file must fail")
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "keep" {
		t.Errorf("existing destination was modified: %q", b)
	}
}
