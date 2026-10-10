package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var proofStore *shellProofStore

func proofFixture(t *testing.T) (dir, exe string, launches *atomic.Int32) {
	t.Helper()
	dir = t.TempDir()
	proofStore = newShellProofStore(dir)
	restart()
	exe = filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(exe, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, exe, new(atomic.Int32)
}

var proofMemo = new(sync.Map)

// restart forgets what this process proved, as a new launch would.
func restart() { proofMemo = new(sync.Map) }

// prove runs one resolution against the fixture's store and the current memo.
func prove(path string, run func(string) bool) bool {
	return proveBash(path, run, proofStore, proofMemo)
}

func countingRun(n *atomic.Int32, ok bool) func(string) bool {
	return func(string) bool { n.Add(1); return ok }
}

func proofFile(dir string) string { return filepath.Join(dir, "shell", "bash-proofs.json") }

func TestShellProofSurvivesARestart(t *testing.T) {
	dir, exe, n := proofFixture(t)
	if !prove(exe, countingRun(n, true)) {
		t.Fatal("working bash rejected")
	}
	restart()
	if !prove(exe, countingRun(n, true)) || n.Load() != 1 {
		t.Fatalf("launches = %d, want the second launch served from the proof", n.Load())
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(proofFile(dir)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("proof file mode = %v, err %v, want 0600", fi.Mode().Perm(), err)
		}
	}
}

func TestShellProofIsNotKeptForAFailure(t *testing.T) {
	dir, exe, n := proofFixture(t)
	if prove(exe, countingRun(n, false)) {
		t.Fatal("failing bash accepted")
	}
	if _, err := os.Stat(proofFile(dir)); err == nil {
		t.Fatal("a failed probe left a proof behind")
	}
}

func TestShellProofMissesWhenTheExecutableChanges(t *testing.T) {
	cases := map[string]func(t *testing.T, exe string, mtime time.Time){
		"same size and mtime, other bytes": func(t *testing.T, exe string, mtime time.Time) {
			if err := os.WriteFile(exe, []byte("two"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(exe, mtime, mtime); err != nil {
				t.Fatal(err)
			}
		},
		"other size": func(t *testing.T, exe string, mtime time.Time) {
			if err := os.WriteFile(exe, []byte("longer"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(exe, mtime, mtime); err != nil {
				t.Fatal(err)
			}
		},
		"other mtime": func(t *testing.T, exe string, mtime time.Time) {
			later := mtime.Add(time.Hour)
			if err := os.Chtimes(exe, later, later); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, exe, n := proofFixture(t)
			fi, _ := os.Stat(exe)
			prove(exe, countingRun(n, true))
			restart()
			change(t, exe, fi.ModTime())
			prove(exe, countingRun(n, true))
			if n.Load() != 2 {
				t.Fatalf("launches = %d, want a changed executable proved again", n.Load())
			}
		})
	}
}

func TestShellProofMissesWhenTheStoreIsTamperedWith(t *testing.T) {
	cases := map[string]func(t *testing.T, loc string){
		"garbage": func(t *testing.T, loc string) {
			if err := os.WriteFile(loc, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"edited entry, stale sum": func(t *testing.T, loc string) {
			b, _ := os.ReadFile(loc)
			for i := range b {
				if b[i] == '1' {
					b[i] = '2'
					break
				}
			}
			if err := os.WriteFile(loc, b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"oversize": func(t *testing.T, loc string) {
			if err := os.WriteFile(loc, make([]byte, shellProofMaxFile+1), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			dir, exe, n := proofFixture(t)
			prove(exe, countingRun(n, true))
			restart()
			tamper(t, proofFile(dir))
			prove(exe, countingRun(n, true))
			if n.Load() != 2 {
				t.Fatalf("launches = %d, want a tampered store distrusted", n.Load())
			}
		})
	}
}

func TestShellProofIsNotRecordedForAFileSwappedDuringTheProbe(t *testing.T) {
	_, exe, n := proofFixture(t)
	fi, _ := os.Stat(exe)
	swap := func(string) bool {
		n.Add(1)
		if err := os.WriteFile(exe, []byte("two"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(exe, fi.ModTime(), fi.ModTime()); err != nil {
			t.Fatal(err)
		}
		return true
	}
	prove(exe, swap)
	restart()
	prove(exe, countingRun(n, true))
	if n.Load() != 2 {
		t.Fatalf("launches = %d, want the swapped file proved again, not vouched for", n.Load())
	}
}

func TestShellProofExpires(t *testing.T) {
	_, exe, n := proofFixture(t)
	prove(exe, countingRun(n, true))
	restart()
	prev := proofStore.now
	proofStore.now = func() time.Time { return time.Now().Add(shellProofTTL + time.Hour) }
	t.Cleanup(func() { proofStore.now = prev })
	prove(exe, countingRun(n, true))
	if n.Load() != 2 {
		t.Fatalf("launches = %d, want an expired proof ignored", n.Load())
	}
}

func TestShellProofIgnoresALinkedStore(t *testing.T) {
	dir, exe, n := proofFixture(t)
	prove(exe, countingRun(n, true))
	real := t.TempDir()
	if err := os.Rename(proofFile(dir), filepath.Join(real, "bash-proofs.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "bash-proofs.json"), proofFile(dir)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	restart()
	prove(exe, countingRun(n, true))
	if n.Load() != 2 {
		t.Fatalf("launches = %d, want a linked proof file ignored", n.Load())
	}
	if err := os.RemoveAll(filepath.Join(dir, "shell")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "shell")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	restart()
	prove(exe, countingRun(n, true))
	if n.Load() != 3 {
		t.Fatalf("launches = %d, want a linked store directory ignored", n.Load())
	}
	entries, _ := os.ReadDir(real)
	if len(entries) != 1 {
		t.Fatalf("a write followed the link into %d entries", len(entries))
	}
}

func TestShellProofKeepsOtherExecutables(t *testing.T) {
	_, exe, n := proofFixture(t)
	other := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(other, []byte("zz"), 0o700); err != nil {
		t.Fatal(err)
	}
	prove(exe, countingRun(n, true))
	prove(other, countingRun(n, true))
	restart()
	prove(exe, countingRun(n, true))
	prove(other, countingRun(n, true))
	if n.Load() != 2 {
		t.Fatalf("launches = %d, want both proofs kept", n.Load())
	}
}

func hostWith(cands []string, probe func(string) bool, budget time.Duration) shellHost {
	return shellHost{search: &bashSearch{budget: budget, hedge: 20 * time.Millisecond}, goos: "windows", lookPath: func(string) (string, error) { return "", os.ErrNotExist }, exists: func(string) bool { return true }, winBash: cands, probe: probe, isWSL: func(string) bool { return false }, launches: func(string) bool { return false }}
}

func TestBashDiscoveryLaunchesOneProbeWhenTheFirstAnswers(t *testing.T) {
	var n atomic.Int32
	h := hostWith([]string{"a", "b"}, func(string) bool { n.Add(1); return true }, time.Second)
	if sh, ok := h.bash(); !ok || sh.Path != "a" || n.Load() != 1 {
		t.Fatalf("got %v %v after %d probes, want a alone", sh, ok, n.Load())
	}
}

func TestBashDiscoveryKeepsPreferenceOrderWhenALaterOneAnswersFirst(t *testing.T) {
	h := hostWith([]string{"slow", "fast"}, func(p string) bool {
		if p == "slow" {
			time.Sleep(150 * time.Millisecond)
		}
		return true
	}, time.Second)
	if sh, ok := h.bash(); !ok || sh.Path != "slow" {
		t.Fatalf("got %v %v, want the preferred candidate", sh, ok)
	}
}

func TestBashDiscoveryDoesNotSerialiseHungCandidates(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := hostWith([]string{"hang1", "hang2", "hang3"}, func(string) bool { <-release; return true }, 400*time.Millisecond)
	start := time.Now()
	if _, ok := h.bash(); ok {
		t.Fatal("hung candidates were accepted")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("discovery took %v, want the shared budget", d)
	}
	for _, p := range []string{"hang1", "hang2", "hang3"} {
		if !bashProbeTimedOut(p) {
			t.Fatalf("%s not recorded as probe_timeout", p)
		}
	}
}

func TestBashDiscoveryTakesALaterProvenCandidateAtTheBudget(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := hostWith([]string{"hang", "good"}, func(p string) bool {
		if p == "hang" {
			<-release
		}
		return true
	}, 300*time.Millisecond)
	if sh, ok := h.bash(); !ok || sh.Path != "good" {
		t.Fatalf("got %v %v, want the proven later candidate", sh, ok)
	}
}

func TestBashDiscoveryFallsBackToPowerShellWithATimeoutReason(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := hostWith([]string{"hang"}, func(string) bool { <-release; return true }, 200*time.Millisecond)
	h.winPS = []string{`C:\ps\powershell.exe`}
	h.launches = func(string) bool { return true }
	sh := h.auto(nil)
	if sh.Kind != ShellPowerShell || sh.Fallback != FallbackProbeTimeout {
		t.Fatalf("got %+v, want PowerShell with probe_timeout", sh)
	}
}

func TestShellDiscoveryResolveProvesOnceAcrossRestarts(t *testing.T) {
	bin := t.TempDir()
	name := "bash"
	if runtime.GOOS == "windows" {
		name = "bash.exe"
	}
	exe := filepath.Join(bin, name)
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var n atomic.Int32
	d := ShellDiscovery{Prefer: "bash", ProofDir: t.TempDir(), Probe: countingRun(&n, true)}
	for range 2 {
		if sh := d.Resolve(); sh.Kind != ShellBash || !strings.EqualFold(sh.Path, exe) {
			t.Fatalf("resolved %+v, want %s", sh, exe)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("probe launched %d times across two resolutions, want 1", n.Load())
	}
}

func TestShellProofStoreWithoutADirectoryHoldsNothing(t *testing.T) {
	if s := newShellProofStore("  "); s != nil {
		t.Fatalf("store for a blank directory = %v, want nil", s)
	}
	_, exe, _ := proofFixture(t)
	fi, _ := os.Stat(exe)
	var none *shellProofStore
	none.record(exe, fi, "x")
	if none.holds(exe, fi) {
		t.Fatal("a nil store vouched for an executable")
	}
}

func TestDigestFileOfAMissingPathFails(t *testing.T) {
	if _, ok := digestFile(filepath.Join(t.TempDir(), "absent")); ok {
		t.Fatal("digest of a missing file reported ok")
	}
}
