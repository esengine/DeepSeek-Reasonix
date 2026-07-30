package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestDefaultSystemPromptStaysLean(t *testing.T) {
	if len(DefaultSystemPrompt) > 240 {
		t.Fatalf("default system prompt grew to %d bytes; keep workflows in mode contracts and tool descriptions", len(DefaultSystemPrompt))
	}
	for _, duplicate := range []string{"todo_write", "plan mode", "verify with tools", "acceptance criteria"} {
		if strings.Contains(strings.ToLower(DefaultSystemPrompt), duplicate) {
			t.Fatalf("default system prompt duplicates %q workflow guidance: %q", duplicate, DefaultSystemPrompt)
		}
	}
	for _, want := range []string{"Reasonix", "available tools", "focused", "concise"} {
		if !strings.Contains(DefaultSystemPrompt, want) {
			t.Fatalf("default system prompt missing %q: %q", want, DefaultSystemPrompt)
		}
	}
}

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

func TestResolveSystemPromptForRootDecodesGB18030(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "prompts", "session.md")
	if err := os.WriteFile(path, fileencoding.Encode(" 请始终使用中文回答。 \n", fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.Agent.SystemPromptFile = filepath.Join("prompts", "session.md")

	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "请始终使用中文回答。" {
		t.Fatalf("system prompt = %q, want decoded Chinese prompt", got)
	}
}
