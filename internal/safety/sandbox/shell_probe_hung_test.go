package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func hungBash(t *testing.T) string {
	t.Helper()
	path := filepath.Join(testenv.TempDir(t), "bash.exe")
	if err := os.WriteFile(path, []byte("hangs"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bashProbeFailures.Delete(path) })
	return path
}

func hangingRun(launches *int) func(string) bool {
	return func(p string) bool {
		*launches++
		bashProbeFailures.Store(p, FallbackProbeTimeout)
		return false
	}
}

func TestProbeBashMemoDoesNotRelaunchAProbeThatHung(t *testing.T) {
	path := hungBash(t)
	launches := 0
	run := hangingRun(&launches)
	for range 3 {
		if probeBashMemo(path, run) {
			t.Fatal("a hung probe reported success")
		}
	}
	if launches != 1 {
		t.Fatalf("launches = %d, want the hang paid once", launches)
	}
	if !bashProbeTimedOut(path) {
		t.Fatal("the remembered hang lost its reason")
	}
}

func TestProbeBashMemoRetriesAHungProbeAfterTheWindowAndOnAChangedFile(t *testing.T) {
	old := hungProbeMemory
	t.Cleanup(func() { hungProbeMemory = old })
	path := hungBash(t)
	launches := 0
	run := hangingRun(&launches)

	hungProbeMemory = 20 * time.Millisecond
	probeBashMemo(path, run)
	time.Sleep(40 * time.Millisecond)
	probeBashMemo(path, run)
	if launches != 2 {
		t.Fatalf("after the window: launches = %d, want 2", launches)
	}

	hungProbeMemory = time.Hour
	probeBashMemo(path, run)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	probeBashMemo(path, run)
	if launches != 3 {
		t.Fatalf("after the file changed: launches = %d, want 3", launches)
	}
}

func TestProbeBashMemoAnswersYesOnceTheFileProvesItself(t *testing.T) {
	old := hungProbeMemory
	t.Cleanup(func() { hungProbeMemory = old })
	hungProbeMemory = time.Millisecond
	path := hungBash(t)
	launches := 0
	probeBashMemo(path, hangingRun(&launches))
	time.Sleep(5 * time.Millisecond)
	if !probeBashMemo(path, func(string) bool { launches++; return true }) {
		t.Fatal("a file that now answers stayed marked as hung")
	}
	if !probeBashMemo(path, func(string) bool { launches++; return false }) || launches != 2 {
		t.Fatalf("a proven file was probed again (launches=%d)", launches)
	}
}
