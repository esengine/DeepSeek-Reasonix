package main

import (
	"context"
	"os/exec"
	"testing"
)

// TestProjectRemoteURLIntegration exercises the full git -> sanitize -> cache
// path with a real temporary repository, mirroring what the desktop UI does
// when the "Open in GitHub" menu item is opened.
func TestProjectRemoteURLIntegration(t *testing.T) {
	repo := t.TempDir()
	runGitRemote(t, repo, "init", "-q")
	runGitRemote(t, repo, "remote", "add", "origin", "https://user:ghp_token123@github.com/acme/widget.git")

	a := &App{ctx: context.Background()}

	// First call: sanitized HTTPS URL with credentials stripped.
	if got := a.ProjectRemoteURL(repo); got != "https://github.com/acme/widget" {
		t.Fatalf("ProjectRemoteURL = %q, want %q", got, "https://github.com/acme/widget")
	}

	// The result is cached with a TTL: after removing the remote the cached
	// value must still be served (the menu keeps working until expiry).
	runGitRemote(t, repo, "remote", "remove", "origin")
	if got := a.ProjectRemoteURL(repo); got != "https://github.com/acme/widget" {
		t.Fatalf("cached ProjectRemoteURL = %q, want %q", got, "https://github.com/acme/widget")
	}

	// A directory that is not a git repository yields an empty result.
	if got := a.ProjectRemoteURL(t.TempDir()); got != "" {
		t.Fatalf("non-repo ProjectRemoteURL = %q, want empty", got)
	}

	// Blank paths are rejected without touching git.
	if got := a.ProjectRemoteURL("  "); got != "" {
		t.Fatalf("blank ProjectRemoteURL = %q, want empty", got)
	}
}

func runGitRemote(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
