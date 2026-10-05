package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

// Resolving a git launcher names its repository by URL, so the configuration
// of whatever repository the process happens to sit in must not reach it.
func TestResolveGitLocatorIgnoresTheWorkingDirectoryRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	marker := filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "core.sshCommand", payload).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v: %s", err, out)
	}
	t.Setenv("GIT_SSH_COMMAND", "")
	_ = os.Unsetenv("GIT_SSH_COMMAND")
	t.Chdir(repo)

	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false binary")
	}
	t.Setenv("GIT_SSH", falseBin)
	if _, _, err := resolveGitLocator(context.Background(), Spec{}, "git+ssh://example.invalid/server.git@main"); err == nil {
		t.Fatal("resolution succeeded against an unreachable remote")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("resolving a launcher ran a program named by the working directory's repository")
	}
}
