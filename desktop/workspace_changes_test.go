package main

import (
	"runtime"
	"slices"
	"testing"
)

func TestWorkspaceGitDisablesDaemonSpawns(t *testing.T) {
	cmd := workspaceGit("-C", "repo", "status", "--porcelain=v1")
	want := []string{"git", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-C", "repo", "status", "--porcelain=v1"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	if runtime.GOOS == "windows" && cmd.SysProcAttr == nil {
		t.Fatal("workspaceGit must hide the console window on Windows")
	}
}
