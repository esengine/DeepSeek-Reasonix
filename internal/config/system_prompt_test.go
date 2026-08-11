package config

import (
	"fmt"
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
	t.Setenv("REASONIX_HOME", t.TempDir())
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

func TestResolveSystemPromptForRootMissingFile(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	cfg := Default()
	cfg.Agent.SystemPromptFile = "prompts/does-not-exist.md"

	_, err := cfg.ResolveSystemPromptForRoot(root)
	if err == nil {
		t.Fatal("expected error for missing system_prompt_file")
	}
	if !strings.Contains(err.Error(), "not found at any configured location") {
		t.Fatalf("error %q missing friendly not-found message", err)
	}
	// All probed locations must be listed so users can see where it looked.
	for _, tried := range []string{filepath.Join(root, "prompts", "does-not-exist.md"), filepath.Join(home, "prompts", "does-not-exist.md")} {
		if !strings.Contains(err.Error(), tried) {
			t.Fatalf("error %q does not list tried path %q", err, tried)
		}
	}
	if got := cfg.InlineSystemPrompt(); got != DefaultSystemPrompt {
		t.Fatalf("InlineSystemPrompt fallback = %q, want DefaultSystemPrompt", got)
	}
}

func TestResolveSystemPromptProjectCannotReadReasonixHome(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	secret := "OTHER_PROVIDER_SECRET=must-not-enter-prompt"
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[agent]\nsystem_prompt_file = \".env\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err == nil {
		t.Fatalf("ResolveSystemPromptForRoot = %q, want missing workspace file error", got)
	}
	if !IsMissingSystemPromptFile(err) {
		t.Fatalf("error = %v, want missing prompt classification", err)
	}
	if strings.Contains(err.Error(), filepath.Join(home, ".env")) || strings.Contains(got, secret) {
		t.Fatalf("project prompt resolution probed Reasonix credentials: got=%q err=%v", got, err)
	}
}

func TestMergeFileSnapshotKeepsProjectPromptSourceAtomic(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	const secret = "PROVIDER_SECRET=must-not-enter-system-prompt"
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectConfig, []byte("[agent]\nsystem_prompt_file = \".env\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	reads := 0
	meta, err := mergeFileSnapshotWithRead(cfg, projectConfig, func(path string) ([]byte, error) {
		reads++
		data, err := fileencoding.ReadFileUTF8(path)
		if err != nil {
			return nil, err
		}
		// Simulate an atomic config replacement after the read. The merged value
		// and its provenance must still come from data, not from a second read.
		if err := os.WriteFile(path, []byte("[agent]\nsystem_prompt = \"replacement\"\n"), 0o600); err != nil {
			return nil, err
		}
		return data, nil
	})
	if err != nil {
		t.Fatalf("mergeFileSnapshotWithRead: %v", err)
	}
	if reads != 1 {
		t.Fatalf("config reads = %d, want exactly one", reads)
	}
	if !meta.IsDefined("agent", "system_prompt_file") {
		t.Fatal("snapshot metadata lost project system_prompt_file")
	}
	cfg.systemPromptFileSource = promptFileSourceProject

	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err == nil || !IsMissingSystemPromptFile(err) {
		t.Fatalf("ResolveSystemPromptForRoot = %q, %v; want project-scoped missing error", got, err)
	}
	if strings.Contains(got, secret) || strings.Contains(err.Error(), filepath.Join(home, ".env")) {
		t.Fatalf("project prompt resolution probed Reasonix credentials: got=%q err=%v", got, err)
	}
}

func TestResolveSystemPromptUserConfigCanFallBackToReasonixHome(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[agent]\nsystem_prompt_file = \"prompts/system.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "prompts", "system.md"), []byte("trusted home prompt"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "trusted home prompt" {
		t.Fatalf("system prompt = %q, want trusted home prompt", got)
	}
}

func TestResolveSystemPromptProjectPathStaysInWorkspace(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for _, dir := range []string{filepath.Join(home, "prompts"), filepath.Join(root, "prompts")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[agent]\nsystem_prompt_file = \"prompts/system.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "prompts", "system.md"), []byte("home prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[agent]\nsystem_prompt_file = \"prompts/system.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "system.md"), []byte("workspace prompt"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "workspace prompt" {
		t.Fatalf("system prompt = %q, want workspace prompt", got)
	}
}

func TestResolveSystemPromptRejectsProjectPathEscapes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for _, test := range []struct {
		name string
		path func(root string) string
	}{
		{name: "parent", path: func(string) string { return filepath.Join("..", "outside.md") }},
		{name: "absolute", path: func(root string) string { return filepath.Join(filepath.Dir(root), "outside.md") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			outside := test.path(root)
			if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), fmt.Appendf(nil, "[agent]\nsystem_prompt_file = %q\n", outside), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadForRootReadOnly(root)
			if err != nil {
				t.Fatalf("LoadForRootReadOnly: %v", err)
			}
			if _, err := cfg.ResolveSystemPromptForRoot(root); err == nil || IsMissingSystemPromptFile(err) || !strings.Contains(err.Error(), "relative path within the workspace") {
				t.Fatalf("ResolveSystemPromptForRoot error = %v, want fatal containment error", err)
			}
		})
	}
}

func TestResolveSystemPromptRejectsProjectSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "prompts", "system.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[agent]\nsystem_prompt_file = \"prompts/system.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	if got, err := cfg.ResolveSystemPromptForRoot(root); err == nil || IsMissingSystemPromptFile(err) || strings.Contains(got, "outside secret") {
		t.Fatalf("ResolveSystemPromptForRoot = %q, %v; want fatal symlink containment error", got, err)
	}
}

func TestResolveSystemPromptMixedReadErrorsAreNotMissing(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "prompts", "system.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Agent.SystemPromptFile = filepath.Join("prompts", "system.md")
	if _, err := cfg.ResolveSystemPromptForRoot(root); err == nil || IsMissingSystemPromptFile(err) {
		t.Fatalf("ResolveSystemPromptForRoot error = %v, want non-missing read failure", err)
	}
}

func TestResolveSystemPromptForRootFallsBackToReasonixHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "prompts", "system.md"), []byte(" home prompt \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.Agent.SystemPromptFile = filepath.Join("prompts", "system.md")

	// Workspace root has no such file; the Reasonix-home copy must win the probe.
	got, err := cfg.ResolveSystemPromptForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "home prompt" {
		t.Fatalf("system prompt = %q, want %q", got, "home prompt")
	}
}

func TestResolveSystemPromptForRootWorkspaceWins(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for dir, content := range map[string]string{home: "home prompt", root: "workspace prompt"} {
		if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompts", "system.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Default()
	cfg.Agent.SystemPromptFile = filepath.Join("prompts", "system.md")

	got, err := cfg.ResolveSystemPromptForRoot(root)
	if err != nil {
		t.Fatalf("ResolveSystemPromptForRoot: %v", err)
	}
	if got != "workspace prompt" {
		t.Fatalf("system prompt = %q, want workspace copy to win, got %q", got, "workspace prompt")
	}
}

func TestResolveSystemPromptForRootDecodesGB18030(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", t.TempDir())
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
