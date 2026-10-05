package tempdir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sentinel = "late-write.txt"

// The failure this exists to keep: content that is still there when the
// deadline passes is a test that left something running, and it is named so the
// next reader knows what to look for. The refusal is injected rather than
// raced, because a producer driven by a sleep would sometimes lose and make
// this test the flaky one.
func TestContentThatOutlivesTheDeadlineIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sentinel), []byte("still running"), 0o600); err != nil {
		t.Fatal(err)
	}
	refuse := func(string) error { return errors.New("teardown refusal") }
	err := quiesce(dir, errors.New("teardown refusal"), 100*time.Millisecond, refuse)
	if err == nil {
		t.Fatal("a directory that never became removable was accepted")
	}
	if !strings.Contains(err.Error(), sentinel) {
		t.Fatalf("the failure does not name what outlived it: %v", err)
	}
}

// And the behaviour that motivated the wait: a refusal that stops is not a
// leak. Reporting the first frame of one is what made the gate flap.
func TestARefusalThatStopsLetsTheDirectoryQuiesce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "settling")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sentinel), []byte("settling"), 0o600); err != nil {
		t.Fatal(err)
	}
	left := 3
	remove := func(path string) error {
		if left > 0 {
			left--
			return errors.New("teardown refusal")
		}
		return os.RemoveAll(path)
	}
	if err := quiesce(dir, errors.New("teardown refusal"), 2*time.Second, remove); err != nil {
		t.Fatalf("quiesce: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("stat after quiesce = %v, want not-exist", err)
	}
}

func TestAnOrdinaryTreeIsRemovedWithoutWaiting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeDir(root); err != nil {
		t.Fatalf("removeDir: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("stat after removal = %v, want not-exist", err)
	}
}

// A directory already gone is not a failure: the test may have removed it.
func TestARemovalOfSomethingAlreadyGoneIsNotReported(t *testing.T) {
	if err := removeDir(filepath.Join(t.TempDir(), "never-existed")); err != nil {
		t.Fatalf("removeDir: %v", err)
	}
}

func TestOnlyATeardownRefusalIsEligibleToBeWaitedOn(t *testing.T) {
	if teardownRefusal(errors.New("some other failure")) {
		t.Fatal("an unrelated error was treated as teardown settling")
	}
}
