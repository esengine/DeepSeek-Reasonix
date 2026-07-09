//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

// simulateForeignSessionLeaseHolder holds path's lease the way another process
// would: the on-disk info names a foreign writer and a raw flock on a separate
// fd keeps the lock file locked without registering in this process's
// in-process lease bookkeeping (flock excludes other fds even within one
// process, so this faithfully mimics a foreign holder).
func simulateForeignSessionLeaseHolder(t *testing.T, path string) {
	t.Helper()
	if err := agent.SaveSessionLeaseInfo(path, agent.SessionLeaseInfo{
		SessionPath: path,
		WriterID:    "other-host-4242-cafebabe",
		PID:         os.Getpid() + 1,
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveSessionLeaseInfo: %v", err)
	}
	f, err := os.OpenFile(path+".lease.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lease lock: %v", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		t.Fatalf("flock lease lock: %v", err)
	}
	t.Cleanup(func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(path + ".lease.lock")
		_ = os.Remove(path + ".lease.json")
	})
}

func TestDeleteSessionKeepsDuplicateLiveSessionHeldByOtherRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "duplicate-foreign-held.jsonl")
	content := []byte(`{"role":"user","content":"same recovery"}` + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write live session: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}
	if err := os.WriteFile(trashPath, content, 0o644); err != nil {
		t.Fatalf("write trash session: %v", err)
	}
	simulateForeignSessionLeaseHolder(t, path)

	activePath := filepath.Join(dir, "active.jsonl")
	if err := os.WriteFile(activePath, []byte(`{"role":"user","content":"active"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write active session: %v", err)
	}
	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: activePath, Label: "active"})
	defer activeCtrl.Close()
	app := &App{
		tabs:        map[string]*WorkspaceTab{"active": {ID: "active", Scope: "global", Ctrl: activeCtrl, Ready: true}},
		activeTabID: "active",
		tabOrder:    []string{"active"},
	}

	err := app.DeleteSession(filepath.Base(path))
	if err == nil || !strings.Contains(err.Error(), errSessionBusyElsewhere.Error()) {
		t.Fatalf("DeleteSession err = %v, want refusal while a foreign runtime holds the lease", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(content) {
		t.Fatalf("live session must stay intact, got %q err=%v", string(got), readErr)
	}
}

// TestEnsureBlankTabSkipsIndexedTopicHeldByForeignRuntime reproduces #6028:
// a blank topic whose session lease is still held by another runtime (a
// lingering background/tray-hidden instance, or a leftover from a crash) must
// not be handed back to a "new conversation" click. Reusing it would collide
// the new tab with that holder, so every lease-gated switch (effort, model,
// token mode) would fail as "open in another Reasonix window" no matter how
// many times the user retries — because it keeps re-picking the same stuck
// topic instead of ever landing on a fresh one.
func TestEnsureBlankTabSkipsIndexedTopicHeldByForeignRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	stuckTopic, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	globalRoot := globalWorkspaceRoot()
	dir := desktopSessionDir(globalRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	stubPath := filepath.Join(dir, "stuck-blank-stub.jsonl")
	if err := os.WriteFile(stubPath, nil, 0o644); err != nil {
		t.Fatalf("write empty stub: %v", err)
	}
	now := time.Now()
	if err := agent.SaveBranchMetaPreserveUpdated(stubPath, agent.BranchMeta{
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now,
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		TopicID:       stuckTopic.ID,
		TopicTitle:    defaultTopicTitle,
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	simulateForeignSessionLeaseHolder(t, stubPath)

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.TopicID == stuckTopic.ID {
		t.Fatalf("EnsureBlankTab reused topic %q even though its session is held by another runtime", stuckTopic.ID)
	}
}
