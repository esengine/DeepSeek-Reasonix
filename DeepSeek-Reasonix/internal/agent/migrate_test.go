package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

const legacyEventLog = `{"type":"model.turn.started","id":1,"ts":"t","turn":0,"model":"deepseek"}
{"type":"user.message","id":2,"ts":"t","turn":0,"text":"list the files"}
{"type":"model.delta","id":3,"ts":"t","turn":0,"channel":"content","text":"sure"}
{"type":"model.final","id":4,"ts":"t","turn":0,"content":"On it.","toolCalls":[{"id":"call_1","type":"function","function":{"name":"ls","arguments":"{\"path\":\".\"}"}}],"usage":{},"costUsd":0}
{"type":"tool.result","id":5,"ts":"t","turn":0,"callId":"call_1","ok":true,"output":"a.go\nb.go","durationMs":3}
{"type":"model.final","id":6,"ts":"t","turn":0,"content":"There are two files.","toolCalls":[],"usage":{},"costUsd":0}
`

func TestMigrateLegacySessionsReconstructsConversation(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := MigrateLegacySessions(src, dest)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d sessions, want 1", n)
	}

	loaded, err := LoadSession(filepath.Join(dest, "chat-1.jsonl"))
	if err != nil {
		t.Fatalf("reload migrated session: %v", err)
	}
	got := loaded.Messages
	if len(got) != 4 {
		t.Fatalf("message count = %d, want 4 (user, assistant+toolcall, tool, assistant):\n%+v", len(got), got)
	}
	if got[0].Role != provider.RoleUser || got[0].Content != "list the files" {
		t.Errorf("msg0 = %+v, want user 'list the files'", got[0])
	}
	if got[1].Role != provider.RoleAssistant || len(got[1].ToolCalls) != 1 ||
		got[1].ToolCalls[0].ID != "call_1" || got[1].ToolCalls[0].Name != "ls" {
		t.Errorf("msg1 = %+v, want assistant with ls tool call call_1", got[1])
	}
	if got[2].Role != provider.RoleTool || got[2].ToolCallID != "call_1" ||
		got[2].Name != "ls" || got[2].Content != "a.go\nb.go" {
		t.Errorf("msg2 = %+v, want tool result for call_1 named ls", got[2])
	}
	if got[3].Role != provider.RoleAssistant || got[3].Content != "There are two files." {
		t.Errorf("msg3 = %+v, want final assistant text", got[3])
	}
}

func TestMigrateLegacySessionsBackfillsAlongsideExisting(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	os.WriteFile(filepath.Join(dest, "existing.jsonl"), []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Errorf("should back-fill the legacy session even when dest has others, imported %d", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "chat-1.jsonl")); err != nil {
		t.Errorf("legacy session should have been imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "existing.jsonl")); err != nil {
		t.Errorf("pre-existing v1+ session must be left intact: %v", err)
	}
}

func TestMigrateLegacySessionsRunsOnce(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)

	if n, err := MigrateLegacySessions(src, dest); err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v, want 1", n, err)
	}
	// User deletes the imported session, then a second launch happens.
	if err := os.Remove(filepath.Join(dest, "chat-1.jsonl")); err != nil {
		t.Fatal(err)
	}
	if n, err := MigrateLegacySessions(src, dest); err != nil || n != 0 {
		t.Fatalf("second run must be a no-op (marker present): n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "chat-1.jsonl")); !os.IsNotExist(err) {
		t.Errorf("a deleted import must not reappear after the one-time migration")
	}
}

func TestMigrateLegacySessionsSkipsAlreadyImported(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	os.WriteFile(filepath.Join(dest, "chat-1.jsonl"), []byte(`{"role":"user","content":"edited"}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("a same-named existing session must not be overwritten, imported %d", n)
	}
	loaded, err := LoadSession(filepath.Join(dest, "chat-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "edited" {
		t.Errorf("existing same-named session was clobbered: %+v", loaded.Messages)
	}
}

func TestMigrateLegacySessionsNoSrcIsNoop(t *testing.T) {
	n, err := MigrateLegacySessions(filepath.Join(t.TempDir(), "nope"), t.TempDir())
	if err != nil || n != 0 {
		t.Errorf("missing legacy session dir should be a silent no-op, got n=%d err=%v", n, err)
	}
}

func TestMigrateLegacySessionsSkipsEmptyLog(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "empty.events.jsonl"), []byte(`{"type":"model.turn.started","id":1,"ts":"t","turn":0}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("a log with no user/assistant/tool messages should not produce a session, imported %d", n)
	}
}
