//go:build linux

package crashreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// otherProcess starts a live process that is not this one and waits until the
// check can see it. The wait is not politeness: between fork and exec there is
// no `/proc/<pid>/exe` yet, and a record that cannot be read is "cannot tell" to
// the check under test — the opposite of what these cases assert.
func otherProcess(t *testing.T) int {
	t.Helper()
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Skipf("cannot start a second process to stand in for a recycled pid: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})
	pid := child.Process.Pid
	if pid == os.Getpid() {
		t.Skip("the stand-in process was given this one's pid")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); err == nil {
			return pid
		}
		if time.Now().After(deadline) {
			t.Skipf("pid %d never reported its exe", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A dump file is named after the pid of the process writing it, and that number
// is not an identity: pids are recycled, so a number left by a kernel that died
// can belong to something else that is alive now. Liveness alone read that as
// "its writer is still running" and skipped the file on every start, which is
// how an empty one sat in `cli-crash-fatal`.
func TestCaptureFatalDumpsClearsADumpWhosePidWasReused(t *testing.T) {
	home := testenv.TempDir(t)
	dir := filepath.Join(home, fatalDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A live pid that belongs to another program: exactly the state a recycled
	// number leaves behind, so liveness alone reads it as a live writer.
	reused := otherProcess(t)
	path := filepath.Join(dir, strconv.Itoa(reused)+fatalSuffix)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	CaptureFatalDumps(home, "test")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a dump naming pid %d survived, though that pid is another program (%v)", reused, err)
	}
}

// The other half of the same rule: a pid that really is this program counts as a
// live writer, so its file is left alone. A dump is not cleared out from under a
// kernel that is still running.
func TestCaptureFatalDumpsKeepsThisProcessesOwnFile(t *testing.T) {
	home := testenv.TempDir(t)
	dir := filepath.Join(home, fatalDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+fatalSuffix)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	CaptureFatalDumps(home, "test")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("this process's own dump was removed: %v", err)
	}
}
