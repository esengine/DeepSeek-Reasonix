package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWriteDocAllowsRecognizedFiles verifies WriteDoc overwrites a canonical
// scope file (project REASONIX.md) and that it round-trips.
func TestWriteDocAllowsRecognizedFiles(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	set := Load(Options{CWD: proj})

	path := set.DocPath(ScopeProject)
	if _, err := set.WriteDoc(path, "# New project memory\n\nUse spaces."); err != nil {
		t.Fatalf("WriteDoc: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Use spaces.") {
		t.Fatalf("body not written: %q", b)
	}
}

// TestWriteDocRejectsArbitraryPaths is the security guard: the panel must not be
// able to overwrite files that aren't recognized memory docs.
func TestWriteDocRejectsArbitraryPaths(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	set := Load(Options{CWD: proj})

	evil := filepath.Join(proj, "..", "escape.txt")
	if _, err := set.WriteDoc(evil, "pwned"); err == nil {
		t.Fatal("WriteDoc accepted an arbitrary path; it must reject non-memory files")
	}
	if _, err := os.Stat(evil); err == nil {
		t.Fatal("WriteDoc wrote a file it should have refused")
	}
}

// TestWriteDocAllowsDiscoveredDoc verifies an already-discovered doc (e.g. an
// AGENTS.md the user is editing) stays writable even though it isn't a canonical
// REASONIX.md scope target.
func TestWriteDocAllowsDiscoveredDoc(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	agents := filepath.Join(proj, "AGENTS.md")
	mustWrite(t, agents, "original")
	set := Load(Options{CWD: proj})

	if _, err := set.WriteDoc(agents, "edited"); err != nil {
		t.Fatalf("WriteDoc on discovered AGENTS.md: %v", err)
	}
	b, _ := os.ReadFile(agents)
	if strings.TrimSpace(string(b)) != "edited" {
		t.Fatalf("AGENTS.md not updated: %q", b)
	}
}

func TestWriteDocRejectsInstructionSymlinkOutsideWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on common Windows setups")
	}
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	outside := filepath.Join(t.TempDir(), "outside.md")
	mustWrite(t, outside, "keep me")
	agents := filepath.Join(proj, "AGENTS.md")
	if err := os.Symlink(outside, agents); err != nil {
		t.Fatal(err)
	}

	set := Load(Options{CWD: proj})
	if _, err := set.WriteDoc(agents, "overwritten"); err == nil {
		t.Fatal("WriteDoc followed an instruction symlink outside the workspace")
	}
	body, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep me" {
		t.Fatalf("outside target changed to %q", body)
	}
}
