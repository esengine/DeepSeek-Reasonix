package sandbox

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestProbeBashMemoLaunchesAProvenExecutableOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(path, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	launches := 0
	run := func(string) bool { launches++; return true }
	for range 3 {
		if !probeBashMemo(path, run) {
			t.Fatal("probe of a working executable failed")
		}
	}
	if launches != 1 {
		t.Fatalf("launches = %d, want 1", launches)
	}
}

func TestProbeBashMemoReprobesAChangedExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(path, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	launches := 0
	run := func(string) bool { launches++; return true }
	probeBashMemo(path, run)
	if err := os.WriteFile(path, []byte("longer content"), 0o700); err != nil {
		t.Fatal(err)
	}
	probeBashMemo(path, run)
	if launches != 2 {
		t.Fatalf("size change: launches = %d, want 2", launches)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	probeBashMemo(path, run)
	if launches != 3 {
		t.Fatalf("mtime change: launches = %d, want 3", launches)
	}
}

func TestProbeBashMemoNeverKeepsAFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(path, []byte("stub"), 0o700); err != nil {
		t.Fatal(err)
	}
	works := false
	launches := 0
	run := func(string) bool { launches++; return works }
	if probeBashMemo(path, run) {
		t.Fatal("a failing probe reported success")
	}
	works = true
	if !probeBashMemo(path, run) {
		t.Fatal("a probe that now works was answered from the earlier failure")
	}
	if launches != 2 {
		t.Fatalf("launches = %d, want 2", launches)
	}
}

func TestProbeBashMemoUnreadablePathIsProbedEveryTime(t *testing.T) {
	launches := 0
	run := func(string) bool { launches++; return false }
	missing := filepath.Join(t.TempDir(), "absent")
	probeBashMemo(missing, run)
	probeBashMemo(missing, run)
	if launches != 2 {
		t.Fatalf("launches = %d, want 2", launches)
	}
}

func TestProbeBashMemoConcurrentCalls(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.exe"), filepath.Join(dir, "bad.exe")
	for _, p := range []string{good, bad} {
		if err := os.WriteFile(p, []byte("x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(p string) bool { return p == good }
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if !probeBashMemo(good, run) {
				t.Error("working executable failed")
			}
			if probeBashMemo(bad, run) {
				t.Error("failing executable reported success")
			}
		})
	}
	wg.Wait()
	launches := 0
	if probeBashMemo(bad, func(string) bool { launches++; return false }) || launches != 1 {
		t.Fatalf("a failure was stored (launches=%d)", launches)
	}
}
