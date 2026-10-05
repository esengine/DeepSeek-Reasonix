package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The git locator is resolved before the launch is approved, and its
// repository part is attacker-controlled: a value that is not a network URL —
// starting with '-', or naming a local transport — must be rejected, never
// passed to ls-remote as an option or a path.
func TestGitLocatorRejectsNonURLRemotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX payload")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	payload := filepath.Join(dir, "p.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, locator := range []string{
		"git+--upload-pack=" + payload + " x@main",
		"git+ext::" + payload + "@main",
		"git+/tmp/local/repo.git@main",
	} {
		if _, _, err := resolveGitLocator(ctx, Spec{Name: "probe"}, locator); err == nil {
			t.Fatalf("resolveGitLocator(%q) succeeded, want a rejected non-URL remote", locator)
		}
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("ls-remote ran a program named by the launcher locator")
	}
}

func TestGitRemoteSchemeIsCaseInsensitiveAndRefusesHTTP(t *testing.T) {
	for remote, want := range map[string]bool{
		"https://example.test/r.git": true, "HTTPS://example.test/r.git": true, "Ssh://h/r.git": true, "git://h/r.git": true,
		"http://example.test/r.git": false, "/srv/r.git": false, "file:///srv/r.git": false, "--upload-pack=x": false,
	} {
		if got := gitRemoteScheme.MatchString(remote); got != want {
			t.Fatalf("gitRemoteScheme(%q) = %v, want %v", remote, got, want)
		}
	}
}
