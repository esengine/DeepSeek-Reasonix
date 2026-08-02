package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestCloneSessionWaitsForCrossProcessWriter proves the clone takes the
// cross-process file lock: a child process holds the source lock and appends
// the newest event while the clone runs, and the clone must still carry that
// event.
func TestCloneSessionWaitsForCrossProcessWriter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	msgs := []provider.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}
	if err := (&Session{Messages: msgs}).Save(src); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSessionCloneHelperProcess")
	cmd.Env = append(os.Environ(), "CLONE_TEST_SRC="+src)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	waitForFile(t, src+".ready", 20*time.Second)

	dst := filepath.Join(dir, "copy.jsonl")
	cloneErr := make(chan error, 1)
	locked := make(chan struct{})
	cloneLockWaitHook = func() { close(locked) }
	t.Cleanup(func() { cloneLockWaitHook = nil })
	go func() {
		clone, err := CloneSessionToPath(src, dst)
		if err == nil {
			lease := clone.Commit()
			if lease == nil {
				err = errors.New("clone commit did not transfer the destination lease")
			} else {
				lease.Release()
			}
		}
		cloneErr <- err
	}()
	// Release the child only after the clone is known to be waiting on the
	// file lock — deterministic, no sleeps.
	select {
	case <-locked:
	case <-time.After(30 * time.Second):
		t.Fatal("clone never reached the file-lock wait")
	}
	if err := os.WriteFile(src+".go", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-cloneErr:
		if err != nil {
			t.Fatalf("clone while child writer held the lock: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("clone did not complete while the child writer held the lock")
	}
	waitForFile(t, src+".done", 20*time.Second)

	cloned, err := LoadSession(dst)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range cloned.Messages {
		texts = append(texts, m.Content)
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(joined, want) {
			t.Errorf("clone missing %q (child appended while clone waited): %v", want, texts)
		}
	}
}

// TestSessionCloneHelperProcess is re-executed by the parent: it holds the
// source's cross-process file lock, waits for the go signal, appends a new
// event to the authoritative log, then releases.
func TestSessionCloneHelperProcess(t *testing.T) {
	src := os.Getenv("CLONE_TEST_SRC")
	if src == "" {
		t.Skip("helper process only")
	}
	unlock := lockSessionSavePath(src)
	unlockFile, err := lockSessionFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unlockFile(); unlock() }()
	if err := os.WriteFile(src+".ready", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, src+".go", 30*time.Second)
	msgs := []provider.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "assistant", Content: "third"},
	}
	// The authoritative log needs a replace baseline before appends can
	// replay: the baseline is the checkpoint transcript, then the newest
	// turn lands as an append the checkpoint has not caught up to.
	baseDigest, _, err := digestAndSizeSessionMessages(msgs[:2])
	if err != nil {
		t.Fatal(err)
	}
	if err := appendSessionReplaceEvent(src, msgs[:2], baseDigest, 0, "baseline"); err != nil {
		t.Fatal(err)
	}
	digest, _, err := digestAndSizeSessionMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendSessionAppendEvent(src, 2, msgs[2:], digest, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+".done", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
