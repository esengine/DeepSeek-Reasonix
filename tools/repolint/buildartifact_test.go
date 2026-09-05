package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCommand(t *testing.T, root, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureRoot(t *testing.T, gitignore string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBuildArtifactFlagsACommandNothingIgnores(t *testing.T) {
	root := fixtureRoot(t, "/bin/\n/reasonix\n")
	writeCommand(t, root, "tools/newtool")
	got := checkBuildArtifacts(root)
	if len(got) != 1 || got[0].Rule != ruleBuildArtifact {
		t.Fatalf("findings = %v, want one build-artifact", got)
	}
	if got[0].File != "tools/newtool/main.go" {
		t.Fatalf("the finding must land on the command that drops the binary: %+v", got[0])
	}
}

func TestBuildArtifactAcceptsAnIgnoredCommand(t *testing.T) {
	root := fixtureRoot(t, "/newtool\n")
	writeCommand(t, root, "tools/newtool")
	if got := checkBuildArtifacts(root); len(got) != 0 {
		t.Fatalf("an ignored command was flagged: %v", got)
	}
}

// A pattern that is not root-anchored, or that names a path, does not promise
// the repo root stays clean: `newtool/` ignores a directory and `newtool`
// matches anywhere, neither of which is the drop this rule is about.
func TestBuildArtifactRejectsPatternsThatDoNotCoverTheDrop(t *testing.T) {
	for _, pattern := range []string{"newtool\n", "/tools/newtool\n", "/newtool/\n"} {
		root := fixtureRoot(t, pattern)
		writeCommand(t, root, "tools/newtool")
		if got := checkBuildArtifacts(root); len(got) != 1 {
			t.Fatalf("pattern %q was read as covering the drop: %v", pattern, got)
		}
	}
}

// A command in a nested module builds into that module's directory, so the
// root .gitignore is not what covers it.
func TestBuildArtifactStopsAtANestedModule(t *testing.T) {
	root := fixtureRoot(t, "")
	writeCommand(t, root, "sub/cmd/thing")
	if err := os.WriteFile(filepath.Join(root, "sub", "go.mod"), []byte("module sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkBuildArtifacts(root); len(got) != 0 {
		t.Fatalf("a nested module's command was flagged: %v", got)
	}
}

// A library package is not a build drop, and neither is a test-only helper
// that happens to sit beside one.
func TestBuildArtifactIgnoresNonMainPackages(t *testing.T) {
	root := fixtureRoot(t, "")
	dir := filepath.Join(root, "internal", "thing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "thing.go"), []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkBuildArtifacts(root); len(got) != 0 {
		t.Fatalf("a library package was flagged: %v", got)
	}
}

// The rule's whole point is that it reads the package list rather than the
// working directory, so it must hold on a checkout where nothing was built.
func TestBuildArtifactCoversThisRepositoryWithoutBuilding(t *testing.T) {
	if got := checkBuildArtifacts("../.."); len(got) != 0 {
		t.Fatalf("a root-module command drops an unignored binary: %v", got)
	}
}
