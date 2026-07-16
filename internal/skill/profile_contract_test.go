package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubagentSkillIsolationFrontmatterLoadsWithoutRewriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".reasonix", SkillsDirname, "worker.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ndescription: worker\nrunAs: subagent\ninvocation: manual\nmodel: deepseek-pro\neffort: auto\nallowed-tools: read_file, grep\nisolation: worktree\n---\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	store := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := store.Read("worker")
	if !ok {
		t.Fatal("skill not loaded")
	}
	if sk.RunAs != RunSubagent || sk.Model != "deepseek-pro" || sk.Effort != "auto" || sk.Invocation != "manual" {
		t.Fatalf("subagent frontmatter parsed incorrectly: %+v", sk)
	}
	if len(sk.AllowedTools) != 2 || sk.AllowedTools[0] != "read_file" || sk.AllowedTools[1] != "grep" {
		t.Fatalf("allowed tools parsed incorrectly: %#v", sk.AllowedTools)
	}
	if sk.Isolation != "worktree" {
		t.Fatalf("isolation = %q, want worktree", sk.Isolation)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("Read should preserve unknown frontmatter on disk\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestEditableSubagentProfileAcceptsManagedIsolationFrontmatter(t *testing.T) {
	home := t.TempDir()
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	if _, err := store.CreateWithContent("worker", ScopeGlobal, "---\ndescription: worker\nrunAs: subagent\ninvocation: manual\nisolation: worktree\n---\nbody\n"); err != nil {
		t.Fatalf("CreateWithContent: %v", err)
	}
	sk, ok := store.Read("worker")
	if !ok {
		t.Fatal("skill not loaded")
	}
	if err := ValidateEditableSubagentProfile(sk); err != nil {
		t.Fatalf("editable profile should accept managed isolation frontmatter: %v", err)
	}
}
