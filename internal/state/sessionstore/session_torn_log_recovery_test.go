package sessionstore

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// tearEventLogTail is what an unclean exit leaves behind: the last record never
// finished being written, so replay stops before the turn it described.
func tearEventLogTail(t *testing.T, path string, bytes int) {
	t.Helper()
	logPath := store.SessionEventLog(path)
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	if bytes >= len(b) {
		t.Fatalf("tear of %d bytes exceeds the %d-byte log", bytes, len(b))
	}
	if err := os.WriteFile(logPath, b[:len(b)-bytes], 0o600); err != nil {
		t.Fatalf("tear event log: %v", err)
	}
}

func sessionUserTexts(msgs []provider.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			out = append(out, UserMessageText(m))
		}
	}
	return out
}

// A torn record costs the turns it described only if nothing else recorded
// them. The checkpoint beside the log is written from the same save, so an
// exit that tore the log usually left the turn durable there — and opening the
// session on the shorter log alone is what made a crash lose a whole exchange.
func TestTornEventLogKeepsTheTurnsTheCheckpointStillHolds(t *testing.T) {
	for _, tear := range []int{5, 20, 100} {
		t.Run("", func(t *testing.T) {
			dir := testenv.TempDir(t)
			path := filepath.Join(dir, "session.jsonl")
			live := NewSession("sys")
			live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
			live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
			if err := live.SaveSnapshot(path); err != nil {
				t.Fatal(err)
			}
			live.Add(provider.Message{Role: provider.RoleUser, Content: "第二句话", RawContent: "第二句话"})
			live.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
			if err := live.SaveSnapshot(path); err != nil {
				t.Fatal(err)
			}
			tearEventLogTail(t, path, tear)

			msgs, _, damaged, err := loadSessionMessages(path)
			if err != nil {
				t.Fatalf("open after the tear: %v", err)
			}
			if !damaged {
				t.Fatal("a torn log must still report damage so the next save heals it")
			}
			if got := sessionUserTexts(msgs); len(got) != 2 || got[1] != "第二句话" {
				t.Fatalf("users after a %d-byte tear = %q, want the checkpoint's two turns", tear, got)
			}
		})
	}
}

// The checkpoint only speaks for turns the surviving log agrees with. One that
// contradicts it — another writer's file, a restored backup — is not a longer
// version of this transcript and must not be adopted as one.
func TestTornEventLogRefusesACheckpointThatContradictsIt(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第二句话", RawContent: "第二句话"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	tearEventLogTail(t, path, 20)
	if err := writeSessionMessages(path, []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "别处写进来的一句", RawContent: "别处写进来的一句"},
		{Role: provider.RoleAssistant, Content: "no"},
		{Role: provider.RoleUser, Content: "以及另一句", RawContent: "以及另一句"},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, _, damaged, err := loadSessionMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if !damaged {
		t.Fatal("want the torn log still reported as damaged")
	}
	if got := sessionUserTexts(msgs); len(got) != 1 || got[0] != "第一句话" {
		t.Fatalf("users = %q, want only what the surviving log replays", got)
	}
}

// A shorter checkpoint is a read model that has not caught up, never a longer
// history: adopting it would drop the turns the log still replays.
func TestTornEventLogRefusesAShorterCheckpoint(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第二句话", RawContent: "第二句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	tearEventLogTail(t, path, 5)
	if err := writeSessionMessages(path, []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}); err != nil {
		t.Fatal(err)
	}

	msgs, _, _, err := loadSessionMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionUserTexts(msgs); len(got) != 1 || got[0] != "第一句话" {
		t.Fatalf("users = %q, want the log's own prefix", got)
	}
}

// An unclean exit leaves the log torn and the checkpoint ahead of it. Healing
// that on open is only half the job: the turn has to survive the next save,
// which rewrites the log from what was opened.
func TestHealedTornLogSurvivesTheNextSave(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第一句话", RawContent: "第一句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	live.Add(provider.Message{Role: provider.RoleUser, Content: "第二句话", RawContent: "第二句话"})
	live.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	tearEventLogTail(t, path, 20)

	reopened, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reopened.Add(provider.Message{Role: provider.RoleUser, Content: "第三句话", RawContent: "第三句话"})
	if err := reopened.SaveSnapshot(path); err != nil {
		t.Fatalf("save after reopen: %v", err)
	}

	final, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	got := sessionUserTexts(final.Snapshot())
	want := []string{"第一句话", "第二句话", "第三句话"}
	if len(got) != len(want) {
		t.Fatalf("users = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("users = %q, want %q", got, want)
		}
	}
}
