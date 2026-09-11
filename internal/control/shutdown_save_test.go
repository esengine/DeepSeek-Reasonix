package control

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestSnapshotForShutdownAppendsToSessionLogInsteadOfForking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	base := agent.NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "persisted"})
	if err := base.SaveSnapshot(path); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	current, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "shutdown tail"})
	exec := agent.New(nil, nil, current, agent.Options{}, event.Discard)
	sink := &noticeSink{}
	c := New(Options{
		Executor: exec, SessionDir: dir, SessionPath: path, Label: "shutdown", Sink: sink,
		OnSessionRecovered: func(info SessionRecoveryInfo) error {
			t.Errorf("a session log must not enter the recovery handoff: %+v", info)
			return nil
		},
	})
	t.Cleanup(agent.SetSessionFileLockWaitForTest(40*time.Millisecond, 5*time.Millisecond))
	release, err := agent.HoldSessionFileLockForTest(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)

	if err := c.SnapshotForShutdown(); err != nil {
		t.Fatalf("SnapshotForShutdown: %v", err)
	}
	release()
	if c.SessionPath() != path {
		t.Fatalf("session path after shutdown = %q, want %q unchanged", c.SessionPath(), path)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if store.IsSessionTranscriptName(entry.Name()) && entry.Name() != filepath.Base(path) {
			t.Fatalf("shutdown wrote a transcript copy %s", entry.Name())
		}
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) != 2 || heads[1].Kind != agent.HeadKindConcurrent || !heads[1].Selected {
		t.Fatalf("heads = %+v err=%v, want the tail on a concurrent head", heads, err)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot(); len(got) != 3 || got[2].Content != "shutdown tail" {
		t.Fatalf("reloaded transcript = %+v", got)
	}
	if notice, ok := sink.lastNotice(); ok && notice.Code == event.NoticeCodeSessionShutdownRecoveryForked {
		t.Fatalf("shutdown emitted the recovery-copy notice: %+v", notice)
	}
}
