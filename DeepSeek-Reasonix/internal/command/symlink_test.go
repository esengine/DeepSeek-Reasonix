package command

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadFollowsSymlinks verifies command discovery follows symlinked
// directories and symlinked .md files (filepath.WalkDir would skip both).
func TestLoadFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}
	cmdDir := t.TempDir()
	target := t.TempDir()

	// Real command files living outside the commands dir.
	mustWrite(t, filepath.Join(target, "pkg", "deploy.md"), "---\ndescription: d\n---\nrun deploy")
	mustWrite(t, filepath.Join(target, "flat.md"), "---\ndescription: f\n---\nflat body")

	// Symlink a directory and a flat file into the commands dir.
	if err := os.Symlink(filepath.Join(target, "pkg"), filepath.Join(cmdDir, "pkg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(target, "flat.md"), filepath.Join(cmdDir, "linked.md")); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(cmdDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	names := map[string]bool{}
	for _, c := range cmds {
		names[c.Name] = true
	}
	if !names["pkg:deploy"] {
		t.Errorf("command under symlinked directory not discovered; got %v", names)
	}
	if !names["linked"] {
		t.Errorf("symlinked flat command file not discovered; got %v", names)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
