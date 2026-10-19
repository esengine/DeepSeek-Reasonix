package shellrun

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoginEnvironmentMergesLoginShellPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "login-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'banner\\n__REASONIX_LOGIN_PATH__=/login/bin:/shared/bin\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=/current/bin:/shared/bin", "REASONIX_TEST=ok"}
	got := LoginEnvironment(context.Background(), env, shell)
	if envValue(got, "PATH") != "/login/bin:/shared/bin:/current/bin" {
		t.Fatalf("PATH = %q", envValue(got, "PATH"))
	}
	if envValue(got, "REASONIX_TEST") != "ok" {
		t.Fatalf("unrelated environment changed: %v", got)
	}
}

func TestLoginEnvironmentFallsBackWhenShellFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "failing-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=/current/bin"}
	got := LoginEnvironment(context.Background(), env, shell)
	if strings.Join(got, "\x00") != strings.Join(env, "\x00") {
		t.Fatalf("failed probe changed environment: %v", got)
	}
}

func TestMergePathRemovesEmptyAndDuplicateEntries(t *testing.T) {
	if got := mergePath("/a::/b:/a", "/b:/c"); got != "/a:/b:/c" {
		t.Fatalf("mergePath = %q", got)
	}
}
