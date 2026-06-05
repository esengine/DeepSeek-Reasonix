package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeMdDiscovered(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	mustWrite(t, filepath.Join(proj, "CLAUDE.md"), "Rule from CLAUDE.md")

	set := Load(Options{CWD: proj})
	if !strings.Contains(set.Block(), "Rule from CLAUDE.md") {
		t.Fatalf("CLAUDE.md should be discovered and folded in:\n%s", set.Block())
	}
}

func TestSymlinkedAgentAndClaudeDocsComposeOnce(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	mustWrite(t, filepath.Join(proj, "CLAUDE.md"), "Shared symlink guidance")
	if err := os.Symlink("CLAUDE.md", filepath.Join(proj, "AGENTS.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	prompt := Compose("BASE", Load(Options{CWD: proj}))
	if got := strings.Count(prompt, "Shared symlink guidance"); got != 1 {
		t.Fatalf("symlinked memory should be composed once, got %d occurrences:\n%s", got, prompt)
	}
}

func TestDocPathDefaultsToAgents(t *testing.T) {
	proj := t.TempDir()
	set := Load(Options{CWD: proj})
	if got := set.DocPath(ScopeProject); filepath.Base(got) != "AGENTS.md" {
		t.Errorf("fresh project should default to AGENTS.md, got %s", got)
	}
	if got := set.DocPath(ScopeLocal); filepath.Base(got) != "AGENTS.local.md" {
		t.Errorf("fresh local should default to AGENTS.local.md, got %s", got)
	}
}

func TestDocPathPrefersExisting(t *testing.T) {
	proj := t.TempDir()
	// An existing REASONIX.md should keep receiving notes (no split to AGENTS.md).
	if err := os.WriteFile(filepath.Join(proj, "REASONIX.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	set := Load(Options{CWD: proj})
	if got := set.DocPath(ScopeProject); filepath.Base(got) != "REASONIX.md" {
		t.Errorf("should append to the existing REASONIX.md, got %s", got)
	}

	// With only a CLAUDE.md present, that's the target.
	proj2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj2, "CLAUDE.md"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	set2 := Load(Options{CWD: proj2})
	if got := set2.DocPath(ScopeProject); filepath.Base(got) != "CLAUDE.md" {
		t.Errorf("should append to the existing CLAUDE.md, got %s", got)
	}
}
