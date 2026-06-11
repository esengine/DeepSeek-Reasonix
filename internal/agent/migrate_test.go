package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	n, err := MigrateLegacySessions(src, dest, nil)
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

	n, err := MigrateLegacySessions(src, dest, nil)
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

	if n, err := MigrateLegacySessions(src, dest, nil); err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v, want 1", n, err)
	}
	// User deletes the imported session, then a second launch happens.
	if err := os.Remove(filepath.Join(dest, "chat-1.jsonl")); err != nil {
		t.Fatal(err)
	}
	if n, err := MigrateLegacySessions(src, dest, nil); err != nil || n != 0 {
		t.Fatalf("second run must be a no-op (marker present): n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "chat-1.jsonl")); !os.IsNotExist(err) {
		t.Errorf("a deleted import must not reappear after the one-time migration")
	}
	if _, err := os.Stat(filepath.Join(dest, legacyEventsHomeImportMarker)); err != nil {
		t.Errorf("source-specific import marker missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, legacyImportMarker)); err != nil {
		t.Errorf("legacy compatibility import marker missing: %v", err)
	}
}

func TestMigrateLegacySessionsRoutedPassIgnoresFlatMarkers(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	for _, m := range []string{legacyImportMarker, legacyEventsHomeImportMarker} {
		if err := os.WriteFile(filepath.Join(dest, m), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Flat markers must not block the routed pass — it has to run once for
	// existing upgraders to re-home sessions the flat import stranded (#3937).
	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("routed pass should run despite flat markers, got %d", n)
	}
	if n, err := MigrateLegacySessions(src, dest, nil); err != nil || n != 0 {
		t.Fatalf("routed marker must gate the second run: n=%d err=%v", n, err)
	}
}

func TestMigrateLegacySessionsSourceMarkersAreIndependent(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "appdata-chat.events.jsonl"), []byte(legacyEventLog), 0o644)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, legacyImportMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	const appDataMarker = ".legacy-imported.v0-events-appdata"
	n, err := migrateLegacySessions(src, dest, appDataMarker, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("independent source marker should allow a new source import, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "appdata-chat.jsonl")); err != nil {
		t.Errorf("new source session should have been imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, appDataMarker)); err != nil {
		t.Errorf("new source marker missing: %v", err)
	}
}

func TestMigrateLegacyConfigSourceDoesNotBlockHomeSource(t *testing.T) {
	configSrc := t.TempDir()
	homeSrc := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(homeSrc, "home-chat.events.jsonl"), []byte(legacyEventLog), 0o644)

	if n, err := MigrateLegacySessionsFromConfigDir(configSrc, dest, nil); err != nil || n != 0 {
		t.Fatalf("config source without events: n=%d err=%v, want 0 nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, legacyRoutedConfigImportMarker)); err != nil {
		t.Fatalf("config source marker missing: %v", err)
	}

	if n, err := MigrateLegacySessions(homeSrc, dest, nil); err != nil || n != 1 {
		t.Fatalf("home source after config source: n=%d err=%v, want 1 nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "home-chat.jsonl")); err != nil {
		t.Fatalf("home source should still import after config source marker: %v", err)
	}
}

func TestMigrateLegacySessionsSkipsAlreadyImported(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	os.WriteFile(filepath.Join(dest, "chat-1.jsonl"), []byte(`{"role":"user","content":"edited"}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest, nil)
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
	n, err := MigrateLegacySessions(filepath.Join(t.TempDir(), "nope"), t.TempDir(), nil)
	if err != nil || n != 0 {
		t.Errorf("missing legacy session dir should be a silent no-op, got n=%d err=%v", n, err)
	}
}

func writeLegacyMeta(t *testing.T, srcDir, base, workspace, summary string) {
	t.Helper()
	b, err := json.Marshal(map[string]string{"workspace": workspace, "summary": summary})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, base+".meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacySessionsRoutesByWorkspaceMeta(t *testing.T) {
	src := t.TempDir()
	global := t.TempDir()
	workspace := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	writeLegacyMeta(t, src, "chat-1", workspace, "fix the retry test")

	n, err := MigrateLegacySessions(src, global, router)
	if err != nil || n != 1 {
		t.Fatalf("migrate: n=%d err=%v, want 1 nil", n, err)
	}
	dest := filepath.Join(projRoot, filepath.Base(workspace), "sessions")
	if _, err := os.Stat(filepath.Join(dest, "chat-1.jsonl")); err != nil {
		t.Fatalf("session should land in the workspace dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "chat-1.jsonl")); !os.IsNotExist(err) {
		t.Errorf("session must not also land in the global dir")
	}
	titles, err := os.ReadFile(filepath.Join(dest, ".titles.json"))
	if err != nil {
		t.Fatalf("titles file: %v", err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(titles, &m); err != nil || m["chat-1.jsonl"] != "fix the retry test" {
		t.Errorf("title = %q (err=%v), want legacy summary", m["chat-1.jsonl"], err)
	}
}

func TestMigrateLegacySessionsDeadWorkspaceFallsBackToGlobal(t *testing.T) {
	src := t.TempDir()
	global := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }
	os.WriteFile(filepath.Join(src, "chat-1.events.jsonl"), []byte(legacyEventLog), 0o644)
	writeLegacyMeta(t, src, "chat-1", filepath.Join(src, "no-such-workspace"), "")

	n, err := MigrateLegacySessions(src, global, router)
	if err != nil || n != 1 {
		t.Fatalf("migrate: n=%d err=%v, want 1 nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(global, "chat-1.jsonl")); err != nil {
		t.Errorf("session with a dead workspace should fall back to the global dir: %v", err)
	}
}

func TestMigrateLegacySessionsRehomesFlatImport(t *testing.T) {
	src := t.TempDir()
	global := t.TempDir()
	workspace := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }
	srcLog := filepath.Join(src, "chat-1.events.jsonl")
	os.WriteFile(srcLog, []byte(legacyEventLog), 0o644)
	writeLegacyMeta(t, src, "chat-1", workspace, "fix the retry test")

	// Simulate the old flat import: file in the global dir, mtime stamped from
	// the legacy event log.
	flat := filepath.Join(global, "chat-1.jsonl")
	os.WriteFile(flat, []byte(`{"role":"user","content":"flat-imported"}`+"\n"), 0o644)
	info, err := os.Stat(srcLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(flat, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	n, err := MigrateLegacySessions(src, global, router)
	if err != nil || n != 1 {
		t.Fatalf("migrate: n=%d err=%v, want 1 nil", n, err)
	}
	moved := filepath.Join(projRoot, filepath.Base(workspace), "sessions", "chat-1.jsonl")
	b, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("re-homed session missing: %v", err)
	}
	if !strings.Contains(string(b), "flat-imported") {
		t.Errorf("re-home must move the existing import, not reconstruct: %s", b)
	}
	if _, err := os.Stat(flat); !os.IsNotExist(err) {
		t.Errorf("flat import should be moved out of the global dir")
	}
}

func TestMigrateLegacySessionsKeepsNativeSameNameSession(t *testing.T) {
	src := t.TempDir()
	global := t.TempDir()
	workspace := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }
	srcLog := filepath.Join(src, "chat-1.events.jsonl")
	os.WriteFile(srcLog, []byte(legacyEventLog), 0o644)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(srcLog, old, old); err != nil {
		t.Fatal(err)
	}
	writeLegacyMeta(t, src, "chat-1", workspace, "")

	// A native v1+ session that happens to share the name: mtime won't match
	// the legacy log, so it must stay where it is.
	native := filepath.Join(global, "chat-1.jsonl")
	os.WriteFile(native, []byte(`{"role":"user","content":"native"}`+"\n"), 0o644)

	if _, err := MigrateLegacySessions(src, global, router); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	b, err := os.ReadFile(native)
	if err != nil || !strings.Contains(string(b), "native") {
		t.Errorf("native global session must be left intact: err=%v body=%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(projRoot, filepath.Base(workspace), "sessions", "chat-1.jsonl")); err != nil {
		t.Errorf("legacy session should still be reconstructed into its workspace dir: %v", err)
	}
}

func TestMigrateLegacySessionsSkipsEmptyLog(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "empty.events.jsonl"), []byte(`{"type":"model.turn.started","id":1,"ts":"t","turn":0}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("a log with no user/assistant/tool messages should not produce a session, imported %d", n)
	}
}
