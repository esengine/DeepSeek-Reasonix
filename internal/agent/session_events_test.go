package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func sessionWithTurns(t *testing.T, path string, turns int) *Session {
	t.Helper()
	s := NewSession("sys")
	for i := 0; i < turns; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: "prompt " + strings.Repeat("x", 8)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot turn %d: %v", i, err)
		}
	}
	return s
}

func TestLoadSessionToleratesTornEventLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := sessionWithTurns(t, path, 2)
	want := len(s.Snapshot())

	logPath := store.SessionEventLog(path)
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.Write([]byte(`{"schema_version":1,"type":"append","message_index":5,"mess`)); err != nil {
		t.Fatalf("write torn tail: %v", err)
	}
	f.Close()

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession with torn tail: %v", err)
	}
	if len(loaded.Messages) != want {
		t.Fatalf("messages with torn tail = %d, want %d", len(loaded.Messages), want)
	}
	if !loaded.eventLogDamaged {
		t.Fatal("eventLogDamaged = false, want true for torn tail")
	}
}

func TestSaveSnapshotHealsTornEventLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionWithTurns(t, path, 2)

	logPath := store.SessionEventLog(path)
	intact, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if err := os.WriteFile(logPath, append(intact, []byte(`{"schema_version":1,"type":"ap`)...), 0o644); err != nil {
		t.Fatalf("write torn log: %v", err)
	}

	// A fresh runtime resumes the session and keeps chatting: the save must
	// truncate the torn tail (not bury it) and the appended turn must replay.
	resumed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "after crash"})
	if err := resumed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after torn tail: %v", err)
	}

	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after heal: %v", err)
	}
	if reloaded.eventLogDamaged {
		t.Fatal("event log still damaged after healing save")
	}
	tail := reloaded.Messages[len(reloaded.Messages)-1]
	if tail.Content != "after crash" {
		t.Fatalf("healed tail = %q, want %q", tail.Content, "after crash")
	}
}

func TestLoadSessionIgnoresForeignEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionWithTurns(t, path, 1)

	// A file this build cannot own squats the native log path: undecodable
	// bytes, or a legacy v0.x Claude-style event transcript left in place by
	// migration. It must be read-ignored and never written to.
	logPath := store.SessionEventLog(path)
	foreign := "garbage that is not json\n"
	if err := os.WriteFile(logPath, []byte(foreign), 0o644); err != nil {
		t.Fatalf("write foreign log: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession with foreign log: %v", err)
	}
	if len(loaded.Messages) == 0 {
		t.Fatal("expected checkpoint transcript, got empty session")
	}
	if loaded.eventLogDamaged {
		t.Fatal("foreign log misreported as damaged native log")
	}

	// Saves keep working checkpoint-only and never touch the foreign file.
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "still works"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot with foreign log: %v", err)
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read foreign log: %v", err)
	}
	if string(got) != foreign {
		t.Fatalf("foreign log was modified:\nbefore=%q\nafter=%q", foreign, got)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after checkpoint-only save: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "still works" {
		t.Fatalf("checkpoint-only tail = %q, want %q", got, "still works")
	}
}

func TestForceSaveLeavesLegacyEventTranscriptUntouched(t *testing.T) {
	// The v0.x migration reconstructs sessions from legacy Claude-style
	// ".events.jsonl" transcripts that live in the SAME directory the native
	// session is imported into — i.e. exactly at the native event-log path.
	// The import must succeed, and the user's original file must survive
	// byte-for-byte.
	dir := t.TempDir()
	path := filepath.Join(dir, "chat-1.jsonl")
	legacy := `{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from v0.x"}` + "\n" +
		`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi","toolCalls":[],"usage":{},"costUsd":0}` + "\n"
	logPath := store.SessionEventLog(path)
	if err := os.WriteFile(logPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy transcript: %v", err)
	}

	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello from v0.x"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatalf("force Save beside legacy transcript: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read legacy transcript: %v", err)
	}
	if string(got) != legacy {
		t.Fatalf("legacy transcript modified:\nbefore=%q\nafter=%q", legacy, got)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession of imported session: %v", err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "hi" {
		t.Fatalf("imported transcript = %+v", loaded.Messages)
	}
}

func TestReplayStopsAtBrokenAppendChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionWithTurns(t, path, 1)
	logPath := store.SessionEventLog(path)
	// Append an event whose MessageIndex does not chain onto the transcript.
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.Write([]byte(`{"schema_version":1,"type":"append","message_index":99,"messages":[{"role":"user","content":"lost"}],"created_at":"2026-01-01T00:00:00Z"}` + "\n")); err != nil {
		t.Fatalf("write broken chain: %v", err)
	}
	f.Close()

	replay, err := replaySessionEventLog(logPath)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.damaged {
		t.Fatal("replay.damaged = false, want true for broken chain")
	}
	for _, m := range replay.msgs {
		if m.Content == "lost" {
			t.Fatal("broken-chain record leaked into replayed transcript")
		}
	}
}

func TestReplayRejectsUnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "future.events.jsonl")
	if err := os.WriteFile(logPath, []byte(`{"schema_version":99,"type":"replace","messages":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write future log: %v", err)
	}
	if _, err := replaySessionEventLog(logPath); err == nil {
		t.Fatal("replay of future schema succeeded, want hard error (no silent truncation of newer writers)")
	}
}

func TestForceSaveDoesNotBootstrapEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "one-shot"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(path)); !os.IsNotExist(err) {
		t.Fatalf("force save created an event log (err=%v); one-shot copies must stay single-file", err)
	}
	if _, err := os.Stat(store.SessionEventIndex(path)); !os.IsNotExist(err) {
		t.Fatalf("force save created an event index (err=%v)", err)
	}
}

func TestForceSaveCompactsExistingEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionWithTurns(t, path, 3)

	forced := NewSession("sys")
	forced.Add(provider.Message{Role: provider.RoleUser, Content: "forced state"})
	if err := forced.Save(path); err != nil {
		t.Fatalf("force Save: %v", err)
	}
	events := readSessionEventsForTest(t, path)
	if len(events) != 1 || events[0].Type != sessionEventTypeReplace {
		t.Fatalf("events after force save = %+v, want single replace", events)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "forced state" {
		t.Fatalf("loaded after force save = %+v, want forced transcript", loaded.Messages)
	}
	anchor, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		t.Fatalf("read anchor: %v", err)
	}
	if len(anchor) != 2 || anchor[1].Content != "forced state" {
		t.Fatalf("anchor after force save = %+v, want refreshed", anchor)
	}
}

func TestEventLogCompactionBoundsGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	filler := strings.Repeat("y", 8<<10)
	// Repeated rewrites (each a full replace event) must not grow the log
	// without bound: once past the threshold the log folds to one event.
	for i := 0; i < 60; i++ {
		s.Replace([]provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: filler},
			{Role: provider.RoleAssistant, Content: strings.Repeat("z", i+1)},
		})
		if err := s.SaveRewrite(path); err != nil {
			t.Fatalf("SaveRewrite %d: %v", i, err)
		}
	}
	_, contentBytes, err := digestAndSizeSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	logSize := sessionEventLogSize(path)
	limit := sessionEventLogCompactFloor
	if scaled := contentBytes * sessionEventLogCompactFactor; scaled > limit {
		limit = scaled
	}
	// One post-compaction replace event of slack is allowed.
	if logSize > limit+contentBytes+4096 {
		t.Fatalf("event log grew unbounded: size=%d limit=%d content=%d", logSize, limit, contentBytes)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after compaction churn: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != strings.Repeat("z", 60) {
		t.Fatalf("compacted transcript tail wrong: %q", got[:min(8, len(got))])
	}
	anchor, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		t.Fatalf("read anchor: %v", err)
	}
	if got := anchor[len(anchor)-1].Content; got != strings.Repeat("z", 60) {
		t.Fatal("anchor not refreshed by checkpoint compaction")
	}
}

func TestConcurrentLoadDuringAppendsStaysConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "seed"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("seed SaveSnapshot: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	errCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			loaded, err := LoadSession(path)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if loaded.eventLogDamaged {
				select {
				case errCh <- os.ErrInvalid:
				default:
				}
				return
			}
		}
	}()
	for i := 0; i < 40; i++ {
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 512)})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatalf("concurrent LoadSession failed or saw damage: %v", err)
	default:
	}
}

func TestSessionsShareContentSeesEventLogDivergence(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.jsonl")
	pathB := filepath.Join(dir, "b.jsonl")
	a := sessionWithTurns(t, pathA, 1)
	_ = sessionWithTurns(t, pathB, 1)

	same, err := SessionsShareContent(pathA, pathB)
	if err != nil {
		t.Fatalf("SessionsShareContent: %v", err)
	}
	if !same {
		t.Fatal("identical transcripts reported as different")
	}

	// Grow A through the event log only — the .jsonl checkpoints stay
	// byte-identical, which is exactly the trap byte comparison fell into.
	a.Add(provider.Message{Role: provider.RoleUser, Content: "diverged"})
	if err := a.SaveSnapshot(pathA); err != nil {
		t.Fatalf("SaveSnapshot diverge: %v", err)
	}
	anchorA, _ := os.ReadFile(pathA)
	anchorB, _ := os.ReadFile(pathB)
	if string(anchorA) != string(anchorB) {
		t.Skip("checkpoints diverged on disk; byte-compare trap not reproducible here")
	}
	same, err = SessionsShareContent(pathA, pathB)
	if err != nil {
		t.Fatalf("SessionsShareContent after divergence: %v", err)
	}
	if same {
		t.Fatal("diverged transcripts reported as identical")
	}
}

func TestLoadSessionUserMessagesSeesEventLogTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}

	users, err := LoadSessionUserMessages(path)
	if err != nil {
		t.Fatalf("LoadSessionUserMessages: %v", err)
	}
	if len(users) != 2 || users[0].Text != "first prompt" || users[1].Text != "second prompt" {
		t.Fatalf("user messages = %+v, want both prompts", users)
	}
	if users[1].At.IsZero() {
		t.Fatal("appended prompt lost its event timestamp")
	}
}

func TestSessionContentModTimeTracksEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := sessionWithTurns(t, path, 1)
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age anchor: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "new"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	anchorInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat anchor: %v", err)
	}
	if got := SessionContentModTime(path); !got.After(anchorInfo.ModTime()) {
		t.Fatalf("SessionContentModTime = %v, want newer than stale anchor %v", got, anchorInfo.ModTime())
	}
}
