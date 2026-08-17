package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func bindSessionWriter(t *testing.T, s *Session, path string) *SessionWriter {
	t.Helper()
	w, err := AcquireSessionWriter(path)
	if err != nil {
		t.Fatalf("AcquireSessionWriter: %v", err)
	}
	t.Cleanup(w.Release)
	if err := w.Bind(s, NextSessionWriteGeneration()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return w
}

func snapshotDigest(t *testing.T, s *Session) [32]byte {
	t.Helper()
	digest, err := digestSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	return digest
}

func TestWriterTailDecisionNoOpAfterSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	decision, ok := s.writerTailDecision(path, s.Snapshot(), snapshotDigest(t, s), false)
	if !ok {
		t.Fatal("writer-tail CAS disarmed after own save")
	}
	if !decision.upToDate {
		t.Fatalf("decision = %+v, want upToDate", decision)
	}
}

func TestWriterTailDecisionAppendAfterSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	before := len(s.Snapshot())
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})

	decision, ok := s.writerTailDecision(path, s.Snapshot(), snapshotDigest(t, s), false)
	if !ok {
		t.Fatal("writer-tail CAS disarmed for append")
	}
	if !decision.appendOnly || decision.appendFrom != before {
		t.Fatalf("decision = %+v, want appendOnly from %d", decision, before)
	}
}

func TestWriterTailDecisionDisarmedWithoutWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if _, ok := s.writerTailDecision(path, s.Snapshot(), snapshotDigest(t, s), false); ok {
		t.Fatal("unbound session must not use writer-tail CAS")
	}
}

func TestWriterTailDecisionDisarmedWhenUnpaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	st := s.persistState(path)
	s.markPersisted(path, st.digest, st.version, st.revision, 0)

	if _, ok := s.writerTailDecision(path, s.Snapshot(), snapshotDigest(t, s), false); ok {
		t.Fatal("unpaired recovery baseline must not use writer-tail CAS")
	}
}

func TestWriterTailDecisionDisarmedWhenLogGrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	foreign := NewSession("sys")
	foreign.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	foreign.Add(provider.Message{Role: provider.RoleAssistant, Content: "other"})
	msgs := foreign.Snapshot()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	revision, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatalf("sessionContentRevision: %v", err)
	}
	if err := appendSessionReplaceEvent(path, msgs, digest, revision, "snapshot"); err != nil {
		t.Fatalf("append foreign event: %v", err)
	}

	if _, ok := s.writerTailDecision(path, s.Snapshot(), snapshotDigest(t, s), false); ok {
		t.Fatal("external event-log growth must disarm writer-tail CAS")
	}
}

func TestWriterTailBindAdoptsLoadBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	bindSessionWriter(t, loaded, path)

	decision, ok := loaded.writerTailDecision(path, loaded.Snapshot(), snapshotDigest(t, loaded), false)
	if !ok || !decision.upToDate {
		t.Fatalf("load+bind writer-tail = ok=%v decision=%+v, want upToDate", ok, decision)
	}
}

func TestWriterTailClassifyDoesNotRereadTranscriptBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	logPath := store.SessionEventLog(path)
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	if err := os.WriteFile(logPath, make([]byte, len(body)), 0o644); err != nil {
		t.Fatalf("overwrite event log: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove jsonl: %v", err)
	}

	digest := snapshotDigest(t, s)
	decision, err := s.classifySnapshotWrite(path, s.Snapshot(), digest, 0, false)
	if err != nil {
		t.Fatalf("classifySnapshotWrite: %v", err)
	}
	if !decision.upToDate {
		t.Fatalf("decision = %+v, want upToDate without rereading wiped bodies", decision)
	}
}

func TestWriterBoundSaveHonorsCompatibilityFileLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	held, err := lockSessionFile(path)
	if err != nil {
		t.Fatalf("lockSessionFile: %v", err)
	}
	defer held()
	prevWait, prevPoll := sessionFileLockWait, sessionFileLockPollInterval
	sessionFileLockWait = 40 * time.Millisecond
	sessionFileLockPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		sessionFileLockWait = prevWait
		sessionFileLockPollInterval = prevPoll
	})
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionFileLockHeld) {
		t.Fatalf("writer-bound save error = %v, want ErrSessionFileLockHeld", err)
	}
}
