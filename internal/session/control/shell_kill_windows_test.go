//go:build windows

package control

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// hungTaskkill puts a taskkill.exe that never returns first on PATH, the way a
// wedged system tool (a busy WMI, security software holding the call) behaves.
func hungTaskkill(t *testing.T) {
	t.Helper()
	dir := testenv.TempDir(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\nimport \"time\"\nfunc main() { time.Sleep(10 * time.Minute) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "taskkill.exe"), src)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the fake taskkill: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A PDF extraction past its deadline runs this cancel hook, and Wait does not
// return until the hook has, so a taskkill that never answers must not hold it.
func TestSetShellKillTreeDoesNotWaitForeverOnAHungTaskkill(t *testing.T) {
	hungTaskkill(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "cmd", "/c", "ping", "-n", "300", "127.0.0.1")
	cmd.WaitDelay = time.Second
	setShellKillTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	start := time.Now()
	cancel()
	select {
	case <-done:
		t.Logf("returned after %v", time.Since(start))
	case <-time.After(40 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command was still running 40s after cancel: a hung taskkill held it")
	}
}
