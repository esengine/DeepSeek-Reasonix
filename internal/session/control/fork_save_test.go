package control

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func forkSaveSession() *sessionstore.Session {
	s := sessionstore.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	return s
}

func writeForkCheckpoint(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(ckptDir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir(path), "turn-0.json"), []byte(`{"schemaVersion":2,"turn":0,"msgIndex":0,"files":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkForkRemoved(t *testing.T, path string) {
	t.Helper()
	paths := append([]string{path, ckptDir(path), sessionstore.CleanupPendingPath(path)}, store.SessionSidecarFiles(path)...)
	for _, artifact := range paths {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("fork artifact remains: %s (%v)", artifact, err)
		}
	}
}

func TestSaveForkPublishesOnlyAfterCheckpoints(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "fork.jsonl")
	err := saveFork(path, forkSaveSession(), sessionstore.BranchMeta{ParentID: "parent", Model: "home/chat-a", AgentPreset: "delivery", Mode: "plan"}, func() error {
		if sessionstore.IsVisibleSession(path) {
			t.Fatal("incomplete fork is visible")
		}
		listed, err := sessionstore.ListSessions(dir)
		if err != nil || len(listed) != 0 {
			t.Fatalf("incomplete fork listed: %v, %v", listed, err)
		}
		if err := ReconcileCleanupPending(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := sessionstore.LoadSession(path); err != nil {
			t.Fatalf("reconciliation deleted a live fork: %v", err)
		}
		meta, ok, err := sessionstore.LoadBranchMeta(path)
		if err != nil || !ok || meta.Model != "home/chat-a" || meta.AgentPreset != "delivery" || meta.Mode != "plan" {
			t.Fatalf("settings were not saved before checkpoint copying: %+v, %v", meta, err)
		}
		writeForkCheckpoint(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := sessionstore.ListSessions(dir)
	if err != nil || len(listed) != 1 || listed[0].Path != path {
		t.Fatalf("completed fork not published: %v, %v", listed, err)
	}
	if sessionstore.IsCleanupPending(path) || sessionstore.SessionLeaseHeld(path) {
		t.Fatal("completed fork still marked or leased")
	}
}

func TestSaveForkCleansFailedCheckpointCopy(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "fork.jsonl")
	failure := errors.New("checkpoint copy failed")
	err := saveFork(path, forkSaveSession(), sessionstore.BranchMeta{ParentID: "parent"}, func() error {
		writeForkCheckpoint(t, path)
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("copy failure lost: %v", err)
	}
	checkForkRemoved(t, path)
}

func TestSaveForkKeepsMarkerWhenCleanupFails(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "fork.jsonl")
	failure := errors.New("checkpoint copy failed")
	metaPath := sessionstore.BranchMetaPath(path)
	err := saveFork(path, forkSaveSession(), sessionstore.BranchMeta{}, func() error {
		writeForkCheckpoint(t, path)
		if err := os.Remove(metaPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(metaPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(metaPath, "blocker"), []byte("held"), 0o600); err != nil {
			t.Fatal(err)
		}
		return failure
	})
	var cleanupErr *os.PathError
	if !errors.Is(err, failure) || !errors.As(err, &cleanupErr) {
		t.Fatalf("save and cleanup failures not preserved: %v", err)
	}
	if sessionstore.IsVisibleSession(path) {
		t.Fatal("failed cleanup exposed the fork")
	}
	if err := ReconcileCleanupPending(dir); err == nil {
		t.Fatal("blocked cleanup unexpectedly succeeded")
	}
	if err := os.Remove(filepath.Join(metaPath, "blocker")); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileCleanupPending(dir); err != nil {
		t.Fatal(err)
	}
	checkForkRemoved(t, path)
}

func TestSaveForkDoesNotRemoveExistingSession(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "existing.jsonl")
	s := forkSaveSession()
	if err := s.SaveIfAbsent(path); err != nil {
		t.Fatal(err)
	}
	err := saveFork(path, forkSaveSession(), sessionstore.BranchMeta{}, func() error {
		t.Fatal("checkpoint copying started for an existing session")
		return nil
	})
	if !os.IsExist(err) {
		t.Fatalf("expected collision refusal, got %v", err)
	}
	saved, err := sessionstore.LoadSession(path)
	if err != nil || saved.Len() != 2 || !sessionstore.IsVisibleSession(path) {
		t.Fatalf("existing session changed: %v", err)
	}
}

func TestSaveForkRecoversAfterProcessExit(t *testing.T) {
	const env = "REASONIX_TEST_FORK_CRASH_PATH"
	if path := os.Getenv(env); path != "" {
		_ = saveFork(path, forkSaveSession(), sessionstore.BranchMeta{ParentID: "parent"}, func() error {
			writeForkCheckpoint(t, path)
			fmt.Fprintln(os.Stdout, "fork-ready")
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(19)
			return nil
		})
		t.Fatal("fork did not reach the crash boundary")
	}
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "interrupted.jsonl")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSaveForkRecoversAfterProcessExit$")
	cmd.Env = append(os.Environ(), env+"="+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	scanner := bufio.NewScanner(stdout)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "fork-ready" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("fork helper did not reach crash boundary: %v", scanner.Err())
	}
	if err := ReconcileCleanupPending(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.LoadSession(path); err != nil {
		t.Fatalf("reconciliation deleted another process's live fork: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 19 {
		t.Fatalf("crash helper: %v\n%s", err, stderr.String())
	}
	if sessionstore.IsVisibleSession(path) {
		t.Fatal("crashed fork is visible")
	}
	if err := ReconcileCleanupPending(dir); err != nil {
		t.Fatal(err)
	}
	checkForkRemoved(t, path)
}
