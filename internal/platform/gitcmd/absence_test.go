package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// A folder git will not open is told apart from one git cannot be asked about.
func TestAbsenceNamesWhyAMarkedFolderHasNoRepository(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ./nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), dir)
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("Open of a folder git refuses = %v, want ErrNotRepository kept for callers that only ask that", err)
	}
	absent, detail := AbsenceOf(err)
	if absent != AbsenceRefused || detail == "" {
		t.Fatalf("absence = %q %q, want refused with what git printed", absent, detail)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Open(ctx, dir)
	if absent, _ := AbsenceOf(err); absent != AbsenceUnavailable {
		t.Fatalf("a git that was not asked in time is %q (%v), want unavailable", absent, err)
	}
	if absent, _ := AbsenceOf(nil); absent != "" {
		t.Fatalf("no error is %q, want no absence", absent)
	}
}

func TestFirstLineIsOneBoundedPrintableLine(t *testing.T) {
	if got := firstLine([]byte("fatal: x\x1b[31m\nsecond line")); got != "fatal: x[31m" {
		t.Fatalf("firstLine = %q", got)
	}
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	if got := firstLine(long); len(got) != 160 {
		t.Fatalf("len = %d, want 160", len(got))
	}
}

// A git stopped by the deadline said nothing about the folder: what it left is a
// non-zero exit with no words, which must not read as "git refused this folder".
// Whether the process had already started when the deadline hit is the clock's
// business, so the answer is decided by the context, not by that race.
func TestAGitStoppedByTheDeadlineIsNotARefusal(t *testing.T) {
	stopped := &exec.ExitError{}
	live := context.Background()
	if err := openFailure(live, "/work", stopped); absentOf(err) != AbsenceRefused {
		t.Fatalf("a git that exited non-zero with the context alive = %v, want refused", err)
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	err := openFailure(ended, "/work", stopped)
	if !errors.Is(err, context.Canceled) || absentOf(err) != AbsenceUnavailable {
		t.Fatalf("a git stopped after the context ended = %v (%q), want the context's error and unavailable", err, absentOf(err))
	}
	if err := openFailure(live, "/work", errors.New("exec: git not found")); absentOf(err) != AbsenceUnavailable {
		t.Fatalf("a git that could not start = %v, want unavailable", err)
	}
}

// An ended context never starts a process at all.
func TestOpenDoesNotAskGitWithAnEndedContext(t *testing.T) {
	dir := testenv.TempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open with an ended context = %v, want context.Canceled", err)
	}
}

func absentOf(err error) Absence {
	a, _ := AbsenceOf(err)
	return a
}
