package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSystemPromptForRootRelativePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "session.md"), []byte(" project session prompt \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	cfg := Default()
	cfg.Agent.SystemPromptFile = filepath.Join("prompts", "session.md")

	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "project session prompt" {
		t.Fatalf("system prompt = %q, want %q", got, "project session prompt")
	}
}

func TestResolveSystemPromptForRootAbsolutePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.md")
	if err := os.WriteFile(path, []byte(" absolute session prompt \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	cfg := Default()
	cfg.Agent.SystemPromptFile = path

	got, err := cfg.ResolveSystemPromptForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "absolute session prompt" {
		t.Fatalf("system prompt = %q, want %q", got, "absolute session prompt")
	}
}
