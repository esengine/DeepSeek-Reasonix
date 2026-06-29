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

func TestMigrateLegacySessionsFromExplicitDirIgnoresDefaultMarkers(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "custom-install.jsonl"), []byte(legacyMessageLog), 0o644); err != nil {
		t.Fatal(err)
	}
	writeImportMarkers(dest, legacyRoutedHomeImportMarker, legacyJsonlPassMarker)

	if n, err := MigrateLegacySessions(src, dest, nil); err != nil || n != 0 {
		t.Fatalf("default migrate with markers: n=%d err=%v, want 0 nil", n, err)
	}
	n, err := MigrateLegacySessionsFromExplicitDir(src, dest, nil)
	if err != nil {
		t.Fatalf("explicit migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("explicit imported %d sessions, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "custom-install.jsonl")); err != nil {
		t.Fatalf("explicit imported session missing: %v", err)
	}
	if n, err := MigrateLegacySessionsFromExplicitDir(src, dest, nil); err != nil || n != 0 {
		t.Fatalf("explicit migrate should be source-marker idempotent: n=%d err=%v", n, err)
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

const v1MessageSession = `{"role":"user","content":"recovered after downgrade"}
{"role":"assistant","content":"ok"}
`

// stampMigrated marks src/dest as already through the one-time passes, with the
// routing watermark set to `at`. It mirrors what a completed migration leaves
// behind so the re-home pass (not the full passes) handles the next run.
func stampMigrated(t *testing.T, dest string, at time.Time) {
	t.Helper()
	for _, m := range []string{legacyRoutedHomeImportMarker, legacyJsonlPassMarker, legacyImportMarker} {
		path := filepath.Join(dest, m)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRehomeStrandedSessionAfterDowngrade reproduces #4666: after the one-time
// routing pass completes, a downgrade-to-old-build writes a project session into
// the flat dir. The next upgrade must re-home it into its workspace dir even
// though the routing marker is present.
func TestRehomeStrandedSessionAfterDowngrade(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	projectDest := t.TempDir()
	projectDir := func(root string) string {
		if root == workspace {
			return projectDest
		}
		return ""
	}

	// Migration already ran a day ago.
	past := time.Now().Add(-24 * time.Hour)
	stampMigrated(t, dest, past)

	// The downgraded build then wrote a project session into the flat dir.
	base := "20260101-000000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace, TopicTitle: "downgrade work"}); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260101_000000_000000000_aabbccddeeff"
	writeMigratedSubagentArtifact(t, src, ref, base)

	n, err := MigrateLegacySessions(src, dest, projectDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("stranded project session should be re-homed, imported %d", n)
	}
	if _, err := os.Stat(filepath.Join(projectDest, base+".jsonl")); err != nil {
		t.Errorf("session should land in the project dir: %v", err)
	}
	// The branch sidecar must follow so the sidebar keeps title/topic.
	if _, err := os.Stat(BranchMetaPath(filepath.Join(projectDest, base+".jsonl"))); err != nil {
		t.Errorf("branch meta sidecar should be copied alongside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDest, "subagents", ref+".jsonl")); err != nil {
		t.Errorf("subagent transcript should be copied alongside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDest, "subagents", ref+".meta.json")); err != nil {
		t.Errorf("subagent metadata should be copied alongside: %v", err)
	}
	// Source is never modified.
	if _, err := os.Stat(sessionPath); err != nil {
		t.Errorf("source session must be left intact: %v", err)
	}
}

func TestJsonlPassRoutesBranchMetaWhenJsonlMarkerMissing(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	projectDest := t.TempDir()
	projectDir := func(root string) string {
		if root == workspace {
			return projectDest
		}
		return ""
	}

	past := time.Now().Add(-24 * time.Hour)
	routedMarker := filepath.Join(dest, legacyRoutedHomeImportMarker)
	if err := os.WriteFile(routedMarker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(routedMarker, past, past); err != nil {
		t.Fatal(err)
	}

	base := "20260101-003000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace, TopicTitle: "half-upgraded"}); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260101_003000_000000000_aabbccddeeff"
	writeMigratedSubagentArtifact(t, src, ref, base)

	n, err := MigrateLegacySessions(src, dest, projectDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("branch-meta jsonl session should be imported once, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(projectDest, base+".jsonl")); err != nil {
		t.Fatalf("session should be routed to the project dir while the jsonl marker is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, base+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("project session must not be copied into the global dir: %v", err)
	}
	if _, err := os.Stat(BranchMetaPath(filepath.Join(projectDest, base+".jsonl"))); err != nil {
		t.Fatalf("branch meta sidecar should be copied alongside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDest, "subagents", ref+".jsonl")); err != nil {
		t.Fatalf("subagent transcript should be copied alongside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDest, "subagents", ref+".meta.json")); err != nil {
		t.Fatalf("subagent metadata should be copied alongside: %v", err)
	}
	if n, err := MigrateLegacySessions(src, dest, projectDir); err != nil || n != 0 {
		t.Fatalf("second run should be a no-op: n=%d err=%v", n, err)
	}
}

// TestRehomeLeavesGlobalSessionsAlone guards the main risk: the flat dir is also
// where CLI/global sessions live. A session with no project scope must stay put.
func TestRehomeLeavesGlobalSessionsAlone(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	projectDir := func(string) string { return t.TempDir() }

	stampMigrated(t, dest, time.Now().Add(-24*time.Hour))

	// A global session (no branch meta, no workspace) written post-migration.
	base := "20260101-010000.000000000-deepseek"
	if err := os.WriteFile(filepath.Join(src, base+".jsonl"), []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := MigrateLegacySessions(src, dest, projectDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("a global flat session must not be re-homed, imported %d", n)
	}
}

// TestRehomeIgnoresSessionsOlderThanWatermark ensures a session the user
// imported and then deleted is not resurrected: only files newer than the
// migration watermark are candidates.
func TestRehomeIgnoresSessionsOlderThanWatermark(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	projectDest := t.TempDir()
	projectDir := func(root string) string {
		if root == workspace {
			return projectDest
		}
		return ""
	}

	now := time.Now()
	stampMigrated(t, dest, now) // watermark = now

	// A project session whose mtime predates the watermark (it was already seen
	// by the original pass and the user deleted the import).
	base := "20250101-000000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace}); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(sessionPath, old, old); err != nil {
		t.Fatal(err)
	}

	n, err := MigrateLegacySessions(src, dest, projectDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("a pre-watermark session must not be revived, imported %d", n)
	}
}

// TestRehomeIsIdempotent verifies the second boot does not re-import: the
// destination check skips already-routed sessions and the watermark advances.
func TestRehomeIsIdempotent(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	projectDest := t.TempDir()
	projectDir := func(root string) string {
		if root == workspace {
			return projectDest
		}
		return ""
	}

	stampMigrated(t, dest, time.Now().Add(-24*time.Hour))
	base := "20260101-020000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644)
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace}); err != nil {
		t.Fatal(err)
	}

	if n, _ := MigrateLegacySessions(src, dest, projectDir); n != 1 {
		t.Fatalf("first run should re-home 1, got %d", n)
	}
	if n, err := MigrateLegacySessions(src, dest, projectDir); err != nil || n != 0 {
		t.Fatalf("second run must be a no-op: n=%d err=%v", n, err)
	}
}

func TestRehomeKeepsWatermarkWhenProjectCopyFails(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := func(root string) string {
		if root == workspace {
			return filepath.Join(blocker, "sessions")
		}
		return ""
	}

	past := time.Now().Add(-24 * time.Hour).Round(0)
	stampMigrated(t, dest, past)
	base := "20260101-023000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace}); err != nil {
		t.Fatal(err)
	}

	if n, err := MigrateLegacySessions(src, dest, projectDir); err != nil || n != 0 {
		t.Fatalf("copy failure should not import: n=%d err=%v", n, err)
	}
	info, err := os.Stat(filepath.Join(dest, legacyRoutedHomeImportMarker))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Fatalf("watermark advanced after copy failure: got %s want %s", info.ModTime(), past)
	}
}

func TestRehomeKeepsWatermarkWhenSubagentCopyFails(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	workspace := t.TempDir()
	projectDest := t.TempDir()
	projectDir := func(root string) string {
		if root == workspace {
			return projectDest
		}
		return ""
	}

	past := time.Now().Add(-24 * time.Hour).Round(0)
	stampMigrated(t, dest, past)
	base := "20260101-024000.000000000-deepseek"
	sessionPath := filepath.Join(src, base+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(v1MessageSession), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(sessionPath, BranchMeta{Scope: "project", WorkspaceRoot: workspace}); err != nil {
		t.Fatal(err)
	}
	writeMigratedSubagentArtifact(t, src, "sa_20260101_024000_000000000_aabbccddeeff", base)
	if err := os.MkdirAll(projectDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDest, "subagents"), []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	if n, err := MigrateLegacySessions(src, dest, projectDir); err != nil || n != 1 {
		t.Fatalf("parent session should still import: n=%d err=%v", n, err)
	}
	info, err := os.Stat(filepath.Join(dest, legacyRoutedHomeImportMarker))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Fatalf("watermark advanced after subagent copy failure: got %s want %s", info.ModTime(), past)
	}
}

func writeMigratedSubagentArtifact(t *testing.T, sessionDir, ref, parentSession string) {
	t.Helper()
	subagentDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".jsonl"), []byte(`{"role":"user","content":"sub"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := SubagentMeta{
		Ref:           ref,
		Status:        SubagentCompleted,
		Kind:          "task",
		Name:          "task",
		ParentSession: parentSession,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".meta.json"), data, 0o644); err != nil {
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

const legacyMessageLog = `{"role":"user","content":"hello from v0.x"}
{"role":"assistant","content":"hi there","tool_calls":[{"id":"call_1","name":"read_file","arguments":"{\"path\":\"main.go\"}"}]}
{"role":"tool","tool_call_id":"call_1","name":"read_file","content":"package main"}
{"role":"assistant","content":"I found the file."}
`

func TestMigrateLegacySessionsImportsJsonlOnly(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "acp-chat.jsonl"), []byte(legacyMessageLog), 0o644)
	writeLegacyMeta(t, src, "acp-chat", "", "ACP session about main.go")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d sessions, want 1 (jsonl-only)", n)
	}

	destPath := filepath.Join(dest, "acp-chat.jsonl")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("imported session missing: %v", err)
	}
	if !strings.Contains(string(data), `"hello from v0.x"`) {
		t.Errorf("imported content wrong:\n%s", data)
	}

	// Title from the meta sidecar should be stored.
	titles, err := os.ReadFile(filepath.Join(dest, ".titles.json"))
	if err != nil {
		t.Fatalf("titles file: %v", err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(titles, &m); err != nil || m["acp-chat.jsonl"] != "ACP session about main.go" {
		t.Errorf("title = %q (err=%v), want ACP session about main.go", m["acp-chat.jsonl"], err)
	}

	// Marker must be stamped.
	if _, err := os.Stat(filepath.Join(dest, legacyJsonlPassMarker)); err != nil {
		t.Errorf("jsonl pass marker missing: %v", err)
	}
}

func TestMigrateLegacySessionsPrefersJsonlWhenNewer(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	eventsPath := filepath.Join(src, "chat-1.events.jsonl")
	jsonlPath := filepath.Join(src, "chat-1.jsonl")

	// Write the event log first (older mtime).
	os.WriteFile(eventsPath, []byte(legacyEventLog), 0o644)
	time.Sleep(10 * time.Millisecond) // ensure mtime differs
	// Write the .jsonl second (newer mtime) — it should be preferred.
	os.WriteFile(jsonlPath, []byte(legacyMessageLog), 0o644)
	writeLegacyMeta(t, src, "chat-1", "", "newer jsonl wins")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}

	data, err := os.ReadFile(filepath.Join(dest, "chat-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The .jsonl content ("hello from v0.x") should win over the reconstructed
	// event log content ("list the files").
	if !strings.Contains(string(data), `"hello from v0.x"`) {
		t.Errorf("expected .jsonl content to be preferred:\n%s", data)
	}
}

func TestMigrateLegacySessionsFallsBackToEventsWhenJsonlOlder(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	jsonlPath := filepath.Join(src, "chat-1.jsonl")
	eventsPath := filepath.Join(src, "chat-1.events.jsonl")

	// Write the .jsonl first (older mtime).
	os.WriteFile(jsonlPath, []byte(legacyMessageLog), 0o644)
	time.Sleep(10 * time.Millisecond)
	// Write the events log second (newer mtime) — events should be reconstructed.
	os.WriteFile(eventsPath, []byte(legacyEventLog), 0o644)
	writeLegacyMeta(t, src, "chat-1", "", "events are newer")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}

	data, err := os.ReadFile(filepath.Join(dest, "chat-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The reconstructed event-log content ("list the files") should win because
	// the events log has a newer mtime.
	if !strings.Contains(string(data), `"list the files"`) {
		t.Errorf("expected events content to be reconstructed:\n%s", data)
	}
}

func TestMigrateLegacySessionsImportsJsonlBakFallback(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	// Only a .jsonl.bak — no .jsonl, no .events.jsonl. Should recover from bak.
	os.WriteFile(filepath.Join(src, "recovered.jsonl.bak"), []byte(legacyMessageLog), 0o644)
	writeLegacyMeta(t, src, "recovered", "", "recovered from bak")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1 (.bak recovery)", n)
	}

	data, err := os.ReadFile(filepath.Join(dest, "recovered.jsonl"))
	if err != nil {
		t.Fatalf("recovered session missing: %v", err)
	}
	if !strings.Contains(string(data), `"hello from v0.x"`) {
		t.Errorf("recovered content wrong:\n%s", data)
	}
}

func TestMigrateLegacySessionsSkipsBakWhenJsonlExists(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	// Both .jsonl and .jsonl.bak exist — prefer .jsonl.
	os.WriteFile(filepath.Join(src, "chat.jsonl"), []byte(legacyMessageLog), 0o644)
	os.WriteFile(filepath.Join(src, "chat.jsonl.bak"), []byte(`{"role":"user","content":"stale backup"}`+"\n"), 0o644)
	writeLegacyMeta(t, src, "chat", "", "from jsonl not bak")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}

	data, err := os.ReadFile(filepath.Join(dest, "chat.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"hello from v0.x"`) {
		t.Errorf("expected .jsonl content, not .bak:\n%s", data)
	}
}

func TestMigrateLegacySessionsSkipsNonMessageJsonl(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	// A .jsonl file that is NOT in message format (starts with event-log "id").
	os.WriteFile(filepath.Join(src, "bad.jsonl"), []byte(`{"id":1,"type":"model.turn.started"}`+"\n"), 0o644)

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("non-message .jsonl should be skipped, imported %d", n)
	}
}

func TestMigrateLegacySessionsRecursesIntoSubdirectories(t *testing.T) {
	src := t.TempDir()
	global := t.TempDir()
	workspace := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }

	// Set up a project-scoped subdirectory with sessions.
	subDir := filepath.Join(src, "Users_Yuki_git_polytone-audio-engine")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(subDir, "proj-chat.events.jsonl"), []byte(legacyEventLog), 0o644)
	writeLegacyMeta(t, subDir, "proj-chat", workspace, "project session")

	// Also add a subdirectory that has no session files — should be skipped.
	emptySub := filepath.Join(src, "empty-dir")
	os.MkdirAll(emptySub, 0o755)

	n, err := MigrateLegacySessions(src, global, router)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1 (subdirectory session)", n)
	}

	// Should land in the project session dir, not global.
	projDest := filepath.Join(projRoot, filepath.Base(workspace), "sessions", "proj-chat.jsonl")
	if _, err := os.Stat(projDest); err != nil {
		t.Errorf("subdirectory session not in project dir %s: %v", projDest, err)
	}
	if _, err := os.Stat(filepath.Join(global, "proj-chat.jsonl")); !os.IsNotExist(err) {
		t.Errorf("subdirectory session should not land in global dir")
	}
}

func TestMigrateLegacySessionsJsonlPassIsIdempotent(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	os.WriteFile(filepath.Join(src, "desktop-session.jsonl"), []byte(legacyMessageLog), 0o644)

	// First run imports it.
	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v, want 1", n, err)
	}
	// Delete the imported session.
	os.Remove(filepath.Join(dest, "desktop-session.jsonl"))

	// Second run: jsonl pass marker exists, must not re-import.
	n, err = MigrateLegacySessions(src, dest, nil)
	if err != nil || n != 0 {
		t.Fatalf("second run must be no-op: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "desktop-session.jsonl")); !os.IsNotExist(err) {
		t.Errorf("deleted session must not reappear after jsonl pass marker")
	}
}

func TestMigrateLegacySessionsJsonlPassRunsForExistingUpgrader(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	os.WriteFile(filepath.Join(src, "acp-chat.jsonl"), []byte(legacyMessageLog), 0o644)

	// Simulate an upgrader whose events pass already completed in a prior
	// version: the routed marker is stamped but the v3-jsonl marker is not.
	writeImportMarkers(dest, legacyRoutedHomeImportMarker)

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1 (.jsonl-only must reach existing upgraders)", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "acp-chat.jsonl")); err != nil {
		t.Errorf("jsonl-only session not imported for existing upgrader: %v", err)
	}
}

// legacyNestedFunctionLog uses the OpenAI-style nested-function tool-call format
// that the TS version wrote: name and arguments live under "function".
const legacyNestedFunctionLog = `{"role":"user","content":"read the file"}
{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}],"reasoning_content":"need to read it"}
{"role":"tool","tool_call_id":"call_1","name":"read_file","content":"package main\nfunc main() {}"}
{"role":"assistant","content":"Found the main function."}
`

func TestTransformAndCopyJsonlFlattensNestedToolCalls(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(src, "chat.jsonl"), []byte(legacyNestedFunctionLog), 0o644)
	writeLegacyMeta(t, src, "chat", "", "nested tool calls test")

	n, err := MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}

	// Reload and verify the tool calls are flat, not nested.
	loaded, err := LoadSession(filepath.Join(dest, "chat.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	msgs := loaded.Messages
	if len(msgs) != 4 {
		t.Fatalf("message count = %d, want 4", len(msgs))
	}
	// Message 1: assistant with tool call.
	if len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(msgs[1].ToolCalls))
	}
	tc := msgs[1].ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("tool call id = %q, want call_1", tc.ID)
	}
	if tc.Name != "read_file" {
		t.Errorf("tool call name = %q, want read_file", tc.Name)
	}
	if tc.Arguments != `{"path":"main.go"}` {
		t.Errorf("tool call arguments = %q, want {\"path\":\"main.go\"}", tc.Arguments)
	}
	// Message 2: tool result.
	if msgs[2].Role != provider.RoleTool || msgs[2].ToolCallID != "call_1" || msgs[2].Name != "read_file" {
		t.Errorf("tool result = %+v, want tool result for call_1", msgs[2])
	}
	// Message 3: final assistant text.
	if msgs[3].Content != "Found the main function." {
		t.Errorf("final content = %q", msgs[3].Content)
	}
}
