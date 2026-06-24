package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
	"reasonix/internal/store"
)

func occupyReadFileWithTimeoutSlots(t *testing.T) func() {
	t.Helper()
	filled := 0
	for filled < cap(readFileWithTimeoutSlots) {
		readFileWithTimeoutSlots <- struct{}{}
		filled++
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		for i := 0; i < filled; i++ {
			<-readFileWithTimeoutSlots
		}
	}
	t.Cleanup(release)
	return release
}

// --- loadSessionTitles ---

func TestLoadSessionTitlesMissing(t *testing.T) {
	dir := t.TempDir()
	m := loadSessionTitles(dir)
	if len(m) != 0 {
		t.Errorf("missing file should return empty map, got %v", m)
	}
}

func TestLoadSessionTitlesCorrupt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(sessionTitlesPath(dir), []byte(`{not json`), 0o644)
	m := loadSessionTitles(dir)
	if len(m) != 0 {
		t.Errorf("corrupt file should return empty map, got %v", m)
	}
}

func TestLoadSessionTitlesValid(t *testing.T) {
	dir := t.TempDir()
	data := map[string]string{"session-1.jsonl": "My Session", "session-2.jsonl": "Another"}
	b, _ := json.Marshal(data)
	os.WriteFile(sessionTitlesPath(dir), b, 0o644)
	m := loadSessionTitles(dir)
	if m["session-1.jsonl"] != "My Session" || m["session-2.jsonl"] != "Another" {
		t.Errorf("loaded = %v", m)
	}
}

// --- saveSessionTitles ---

func TestSaveSessionTitlesCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	m := map[string]string{"a.jsonl": "title A"}
	if err := saveSessionTitles(dir, m); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Verify file exists and is valid JSON.
	b, err := os.ReadFile(sessionTitlesPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["a.jsonl"] != "title A" {
		t.Errorf("decoded = %v", decoded)
	}
}

func TestSaveSessionTitlesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[string]string{"s1.jsonl": "First", "s2.jsonl": "Second"}
	if err := saveSessionTitles(dir, original); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := loadSessionTitles(dir)
	if loaded["s1.jsonl"] != "First" || loaded["s2.jsonl"] != "Second" {
		t.Errorf("round-trip = %v", loaded)
	}
}

// --- setSessionTitle ---

func TestSetSessionTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "my-session.jsonl")

	// Set a title.
	if err := setSessionTitle(dir, sessionPath, "Custom Title"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m := loadSessionTitles(dir)
	if m["my-session.jsonl"] != "Custom Title" {
		t.Errorf("title = %q", m["my-session.jsonl"])
	}

	// Clear the title (empty string).
	if err := setSessionTitle(dir, sessionPath, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	m = loadSessionTitles(dir)
	if _, ok := m["my-session.jsonl"]; ok {
		t.Error("cleared title should be removed from map")
	}
}

func TestSetSessionTitleTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := setSessionTitle(dir, sessionPath, "  trimmed  "); err != nil {
		t.Fatalf("set: %v", err)
	}
	m := loadSessionTitles(dir)
	if m["s.jsonl"] != "trimmed" {
		t.Errorf("title = %q, want trimmed", m["s.jsonl"])
	}
}

func TestSetSessionTitlePreservesExistingTitlesWhenTimedReadSlotsFull(t *testing.T) {
	dir := t.TempDir()
	if err := saveSessionTitles(dir, map[string]string{"old.jsonl": "Old"}); err != nil {
		t.Fatalf("save old title: %v", err)
	}
	sessionPath := filepath.Join(dir, "new.jsonl")
	release := occupyReadFileWithTimeoutSlots(t)
	if err := setSessionTitle(dir, sessionPath, "New"); err != nil {
		t.Fatalf("setSessionTitle: %v", err)
	}
	release()

	m := loadSessionTitles(dir)
	if got := m["old.jsonl"]; got != "Old" {
		t.Fatalf("old title = %q, want Old (all titles: %v)", got, m)
	}
	if got := m["new.jsonl"]; got != "New" {
		t.Fatalf("new title = %q, want New (all titles: %v)", got, m)
	}
}

// --- deleteSessionFile ---

func TestDeleteSessionFile(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)
	metaPath := sessionPath + ".meta"
	os.WriteFile(metaPath, []byte("{}"), 0o644)
	goalPath := store.SessionGoalState(sessionPath)
	os.WriteFile(goalPath, []byte(`{"goal":"ship"}`), 0o644)
	telemetryPath := sessionTelemetryPath(sessionPath)
	os.WriteFile(telemetryPath, []byte(`{"version":2,"readFiles":[]}`), 0o644)
	ckptDir := filepath.Join(dir, "session.ckpt")
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatalf("mkdir ckpt: %v", err)
	}
	os.WriteFile(filepath.Join(ckptDir, "1.json"), []byte("{}"), 0o644)
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatalf("mkdir jobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "bash-1.log"), []byte("output"), 0o644); err != nil {
		t.Fatalf("write job artifact: %v", err)
	}

	// Set a title first.
	setSessionTitle(dir, sessionPath, "My Title")
	if err := recordSessionDisplay(dir, sessionPath, "expanded prompt", "[Pasted text #1 · 5 lines]"); err != nil {
		t.Fatalf("record display: %v", err)
	}

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("delete: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	trashMetaPath := trashPath + ".meta"
	trashGoalPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.goal-state.json")
	trashTelemetryPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl.telemetry.json")
	trashCkptDir := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.ckpt")
	trashJobsDir := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jobs")

	// File should be moved out of the active session list.
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file should be removed from active sessions")
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("session meta should be removed from active sessions")
	}
	if _, err := os.Stat(goalPath); !os.IsNotExist(err) {
		t.Error("session goal state should be removed from active sessions")
	}
	if _, err := os.Stat(telemetryPath); !os.IsNotExist(err) {
		t.Error("session telemetry should be removed from active sessions")
	}
	if _, err := os.Stat(ckptDir); !os.IsNotExist(err) {
		t.Error("session checkpoints should be removed from active sessions")
	}
	if _, err := os.Stat(jobsDir); !os.IsNotExist(err) {
		t.Error("session jobs should be removed from active sessions")
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("session file should be in trash: %v", err)
	}
	if _, err := os.Stat(trashMetaPath); err != nil {
		t.Fatalf("session meta should be in trash: %v", err)
	}
	if _, err := os.Stat(trashGoalPath); err != nil {
		t.Fatalf("session goal state should be in trash: %v", err)
	}
	if _, err := os.Stat(trashTelemetryPath); err != nil {
		t.Fatalf("session telemetry should be in trash: %v", err)
	}
	if _, err := os.Stat(trashCkptDir); err != nil {
		t.Fatalf("session checkpoints should be in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trashJobsDir, "bash-1.log")); err != nil {
		t.Fatalf("session jobs should be in trash: %v", err)
	}
	// Title/display should be retained until permanent deletion.
	m := loadSessionTitles(dir)
	if m["session.jsonl"] != "My Title" {
		t.Errorf("title should be retained in trash, got %q", m["session.jsonl"])
	}
	if got := resolveSessionDisplay(dir, sessionPath, "expanded prompt"); got != "[Pasted text #1 · 5 lines]" {
		t.Errorf("display sidecar should be retained in trash, got %q", got)
	}
}

func TestReconcileDesktopCleanupPendingDeleteMovesArtifactsToTrash(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "pending.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"pending"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobs.ArtifactDir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs.ArtifactDir(sessionPath), "job.log"), []byte("job output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcileDesktopCleanupPending: %v", err)
	}

	trashPath := filepath.Join(dir, sessionTrashDir, "pending.jsonl", "pending.jsonl")
	trashJobsDir := filepath.Join(dir, sessionTrashDir, "pending.jsonl", "pending.jobs")
	for _, p := range []string{
		trashPath,
		filepath.Join(trashJobsDir, "job.log"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected trashed artifact %s: %v", p, err)
		}
	}
	for _, p := range []string{sessionPath, jobs.ArtifactDir(sessionPath), agent.CleanupPendingPath(sessionPath)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after reconciliation (err=%v)", p, err)
		}
	}
}

func TestReconcileDesktopCleanupPendingDeleteReusesExistingTrashDir(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "partial.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"partial"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	itemDir := filepath.Join(sessionTrashPath(dir), "partial.jsonl")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcileDesktopCleanupPending: %v", err)
	}

	if _, err := os.Stat(filepath.Join(itemDir, "partial.jsonl")); err != nil {
		t.Fatalf("session should be moved into existing trash dir: %v", err)
	}
	if _, err := os.Stat(agent.CleanupPendingPath(sessionPath)); !os.IsNotExist(err) {
		t.Fatalf("cleanup marker still exists after reconciliation (err=%v)", err)
	}
}

func TestReconcileDesktopCleanupPendingDeleteMovesRemainingSidecars(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "sidecars.jsonl")
	if err := os.WriteFile(sessionPath+".meta", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	goalPath := store.SessionGoalState(sessionPath)
	if err := os.WriteFile(goalPath, []byte(`{"goal":"finish"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	telemetryPath := sessionTelemetryPath(sessionPath)
	if err := os.WriteFile(telemetryPath, []byte(`{"version":2,"readFiles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptDir := filepath.Join(dir, "sidecars.ckpt")
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir, "1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobs.ArtifactDir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs.ArtifactDir(sessionPath), "job.log"), []byte("job output"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	itemDir := filepath.Join(sessionTrashPath(dir), "sidecars.jsonl")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(itemDir, "sidecars.jsonl"), []byte(`{"role":"user","content":"sidecars"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcileDesktopCleanupPending: %v", err)
	}

	for _, p := range []string{
		filepath.Join(itemDir, "sidecars.jsonl.meta"),
		filepath.Join(itemDir, "sidecars.goal-state.json"),
		filepath.Join(itemDir, "sidecars.jsonl.telemetry.json"),
		filepath.Join(itemDir, "sidecars.ckpt", "1.json"),
		filepath.Join(itemDir, "sidecars.jobs", "job.log"),
		filepath.Join(itemDir, "subagents", ref+".jsonl"),
		filepath.Join(itemDir, "subagents", ref+".meta.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected remaining artifact %s to move into trash: %v", p, err)
		}
	}
	for _, p := range []string{
		sessionPath + ".meta",
		goalPath,
		telemetryPath,
		ckptDir,
		jobs.ArtifactDir(sessionPath),
		filepath.Join(dir, "subagents", ref+".jsonl"),
		filepath.Join(dir, "subagents", ref+".meta.json"),
		agent.CleanupPendingPath(sessionPath),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after reconciliation (err=%v)", p, err)
		}
	}
}

func TestDeleteSessionFileMovesOwnedSubagentsToTrash(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)
	writeSubagentArtifact(t, dir, "sa_20260102_030405_000000000_aabbccddeeff", agent.BranchID(sessionPath))
	writeSubagentArtifact(t, dir, "sa_20260102_030405_000000000_112233445566", "other-parent")

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("delete: %v", err)
	}

	ownedJSONL := filepath.Join(dir, "subagents", "sa_20260102_030405_000000000_aabbccddeeff.jsonl")
	ownedMeta := filepath.Join(dir, "subagents", "sa_20260102_030405_000000000_aabbccddeeff.meta.json")
	if _, err := os.Stat(ownedJSONL); !os.IsNotExist(err) {
		t.Fatalf("owned subagent jsonl should be moved out of active dir, stat err = %v", err)
	}
	if _, err := os.Stat(ownedMeta); !os.IsNotExist(err) {
		t.Fatalf("owned subagent meta should be moved out of active dir, stat err = %v", err)
	}
	trashSubagentDir := filepath.Join(dir, sessionTrashDir, "session.jsonl", "subagents")
	if _, err := os.Stat(filepath.Join(trashSubagentDir, "sa_20260102_030405_000000000_aabbccddeeff.jsonl")); err != nil {
		t.Fatalf("owned subagent jsonl should be in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trashSubagentDir, "sa_20260102_030405_000000000_aabbccddeeff.meta.json")); err != nil {
		t.Fatalf("owned subagent meta should be in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", "sa_20260102_030405_000000000_112233445566.jsonl")); err != nil {
		t.Fatalf("unowned subagent jsonl should remain active: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", "sa_20260102_030405_000000000_112233445566.meta.json")); err != nil {
		t.Fatalf("unowned subagent meta should remain active: %v", err)
	}
}

func TestDeleteSessionFileNoTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "no-title.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file should be removed from active sessions")
	}
	if _, err := os.Stat(filepath.Join(dir, sessionTrashDir, "no-title.jsonl", "no-title.jsonl")); err != nil {
		t.Fatalf("session file should be in trash: %v", err)
	}
}

func TestRestoreTrashedSessionFile(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath+".meta", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	goalPath := store.SessionGoalState(sessionPath)
	if err := os.WriteFile(goalPath, []byte(`{"goal":"restore"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	telemetryPath := sessionTelemetryPath(sessionPath)
	if err := os.WriteFile(telemetryPath, []byte(`{"version":2,"readFiles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptDir := filepath.Join(dir, "session.ckpt")
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir, "1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "bash-1.log"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setSessionTitle(dir, sessionPath, "My Title"); err != nil {
		t.Fatal(err)
	}

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	if err := restoreTrashedSessionFile(dir, trashPath); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session file should be restored: %v", err)
	}
	if _, err := os.Stat(sessionPath + ".meta"); err != nil {
		t.Fatalf("session meta should be restored: %v", err)
	}
	if _, err := os.Stat(goalPath); err != nil {
		t.Fatalf("session goal state should be restored: %v", err)
	}
	if _, err := os.Stat(telemetryPath); err != nil {
		t.Fatalf("session telemetry should be restored: %v", err)
	}
	if _, err := os.Stat(ckptDir); err != nil {
		t.Fatalf("session checkpoints should be restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(jobsDir, "bash-1.log")); err != nil {
		t.Fatalf("session jobs should be restored: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(trashPath)); !os.IsNotExist(err) {
		t.Fatalf("trash item should be removed after restore, stat err = %v", err)
	}
	if got := loadSessionTitles(dir)["session.jsonl"]; got != "My Title" {
		t.Fatalf("title should survive restore, got %q", got)
	}
}

func TestRestoreTrashedSessionFileRestoresSubagents(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}

	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	if err := restoreTrashedSessionFile(dir, trashPath); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".jsonl")); err != nil {
		t.Fatalf("subagent jsonl should be restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".meta.json")); err != nil {
		t.Fatalf("subagent meta should be restored: %v", err)
	}
}

func TestRestoreTrashedSessionFileRejectsSubagentConflict(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subagents", ref+".jsonl"), []byte("conflict"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	if err := restoreTrashedSessionFile(dir, trashPath); err == nil {
		t.Fatal("restore should fail on subagent conflict")
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trash item should remain after failed restore: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("parent session should not be restored after conflict, stat err = %v", err)
	}
}

func TestPurgeTrashedSessionFile(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)
	goalPath := store.SessionGoalState(sessionPath)
	os.WriteFile(goalPath, []byte(`{"goal":"purge"}`), 0o644)
	telemetryPath := sessionTelemetryPath(sessionPath)
	os.WriteFile(telemetryPath, []byte(`{"version":2,"readFiles":[]}`), 0o644)
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "bash-1.log"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setSessionTitle(dir, sessionPath, "My Title"); err != nil {
		t.Fatal(err)
	}
	if err := recordSessionDisplay(dir, sessionPath, "expanded prompt", "[Pasted text #1 · 5 lines]"); err != nil {
		t.Fatal(err)
	}
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	if err := purgeTrashedSessionFile(dir, trashPath); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(trashPath)); !os.IsNotExist(err) {
		t.Fatalf("trash item should be removed after purge, stat err = %v", err)
	}
	if _, err := os.Stat(jobsDir); !os.IsNotExist(err) {
		t.Fatalf("session jobs should be removed after purge, stat err = %v", err)
	}
	if _, err := os.Stat(goalPath); !os.IsNotExist(err) {
		t.Fatalf("session goal state should be removed after purge, stat err = %v", err)
	}
	if _, err := os.Stat(telemetryPath); !os.IsNotExist(err) {
		t.Fatalf("session telemetry should be removed after purge, stat err = %v", err)
	}
	if _, ok := loadSessionTitles(dir)["session.jsonl"]; ok {
		t.Fatal("title should be removed after purge")
	}
	if got := resolveSessionDisplay(dir, sessionPath, "expanded prompt"); got != "expanded prompt" {
		t.Fatalf("display sidecar should be removed after purge, got %q", got)
	}
}

func TestPurgeTrashedSessionFileRemovesSubagents(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	if err := purgeTrashedSessionFile(dir, trashPath); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionTrashDir, "session.jsonl", "subagents", ref+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("trashed subagent should be removed by purge, stat err = %v", err)
	}
}

func TestListTrashedSessionFilesRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("data"), 0o644)
	itemDir := filepath.Join(dir, sessionTrashDir, "outside.jsonl")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(itemDir, "outside.jsonl")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := listTrashedSessionFiles(dir)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("trash listing should skip symlink escape, got %v", got)
	}
}

func TestDeleteSessionFileMissing(t *testing.T) {
	dir := t.TempDir()
	// Deleting a non-existent file should not error.
	if err := deleteSessionFile(dir, filepath.Join(dir, "missing.jsonl")); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestRemoveDesktopSessionArtifactsRemovesOwnedSidecars(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	for _, p := range []string{
		sessionPath,
		store.SessionMeta(sessionPath),
		store.SessionGoalState(sessionPath),
		sessionTelemetryPath(sessionPath),
	} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	ckptDir := store.SessionCheckpointDir(sessionPath)
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir, "1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "job.log"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeDesktopSessionArtifacts(sessionPath); err != nil {
		t.Fatalf("removeDesktopSessionArtifacts: %v", err)
	}

	for _, p := range []string{
		sessionPath,
		store.SessionMeta(sessionPath),
		store.SessionGoalState(sessionPath),
		sessionTelemetryPath(sessionPath),
		ckptDir,
		jobsDir,
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed, stat err = %v", p, err)
		}
	}
}

func TestDeleteSessionFileRejectsOutsideDir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("data"), 0o644)

	if err := deleteSessionFile(dir, outside); err == nil {
		t.Fatal("delete outside dir should fail")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file should remain: %v", err)
	}
}

func TestDeleteSessionFileRejectsNonJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.meta")
	os.WriteFile(path, []byte("data"), 0o644)

	if err := deleteSessionFile(dir, path); err == nil {
		t.Fatal("delete non-jsonl should fail")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("non-jsonl file should remain: %v", err)
	}
}

func TestDeleteSessionFileRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("data"), 0o644)
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := deleteSessionFile(dir, link); err == nil {
		t.Fatal("delete symlink escape should fail")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside target should remain: %v", err)
	}
}

func TestMovePathIfExistsCopyFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.txt")

	// Test normal move.
	if err := movePathIfExists(src, dst); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("src should be removed")
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "hello" {
		t.Fatalf("dst content = %q, err = %v", got, err)
	}

	// Test non-existent source is no-op.
	if err := movePathIfExists(filepath.Join(dir, "missing.txt"), filepath.Join(dir, "other.txt")); err != nil {
		t.Fatalf("move missing: %v", err)
	}
}

func TestCopyAndRemoveDirectory(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("file a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("file b"), 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(dir, "dst")

	if err := copyAndRemove(srcDir, dstDir); err != nil {
		t.Fatalf("copyAndRemove: %v", err)
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatal("src dir should be removed")
	}
	if got, err := os.ReadFile(filepath.Join(dstDir, "a.txt")); err != nil || string(got) != "file a" {
		t.Fatalf("dst a.txt = %q, err = %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt")); err != nil || string(got) != "file b" {
		t.Fatalf("dst sub/b.txt = %q, err = %v", got, err)
	}
}

func TestCopyAndRemoveDirectoryPreservesSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	dstDir := filepath.Join(dir, "dst")

	if err := copyAndRemove(srcDir, dstDir); err != nil {
		t.Fatalf("copyAndRemove: %v", err)
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatal("src dir should be removed")
	}
	dstLink := filepath.Join(dstDir, "link.txt")
	info, err := os.Lstat(dstLink)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dst link should remain a symlink, mode=%v", info.Mode())
	}
	target, err := os.Readlink(dstLink)
	if err != nil {
		t.Fatal(err)
	}
	if target != outside {
		t.Fatalf("dst link target = %q, want %q", target, outside)
	}
}

func writeSubagentArtifact(t *testing.T, dir, ref, parentSession string) {
	t.Helper()
	subagentDir := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".jsonl"), []byte(`{"role":"user","content":"sub"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := agent.SubagentMeta{
		Ref:           ref,
		Status:        agent.SubagentCompleted,
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

// --- sessionTitlesPath ---

func TestSessionTitlesPath(t *testing.T) {
	got := sessionTitlesPath("/sessions")
	want := filepath.Join("/sessions", ".titles.json")
	if got != want {
		t.Errorf("sessionTitlesPath = %q, want %q", got, want)
	}
}

func TestSessionDisplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	content := "prefix\n--- Begin [Pasted text #1 · 5 lines] ---\nfull text\n--- End [Pasted text #1 · 5 lines] ---"
	display := "[Pasted text #1 · 5 lines]"
	if err := recordSessionDisplay(dir, sessionPath, content, display); err != nil {
		t.Fatalf("record display: %v", err)
	}
	if got := resolveSessionDisplay(dir, sessionPath, content); got != display {
		t.Fatalf("display = %q, want %q", got, display)
	}
	if got := resolveSessionDisplay(dir, sessionPath, "other"); got != "other" {
		t.Fatalf("unknown content should pass through, got %q", got)
	}
}

func TestRecordSessionDisplaySkipsNoop(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := recordSessionDisplay(dir, sessionPath, "same", "same"); err != nil {
		t.Fatalf("record display: %v", err)
	}
	if _, err := os.Stat(sessionDisplayPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("noop display should not create sidecar, stat err = %v", err)
	}
}
