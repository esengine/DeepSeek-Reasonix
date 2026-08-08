package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestBuildMissingProjectSystemPromptFileSkipsSilently(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Setenv("REASONIX_HOME", robustTempDir(t))
	writeFile(t, root, "reasonix.toml", systemPromptFileTestConfig("prompts/missing.md"))

	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: root,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice && e.Level == event.LevelWarn &&
				strings.Contains(e.Text, "falling back to inline/default system prompt") {
				t.Fatalf("project-scope missing prompt file must skip silently, got warning: %s", e.Text)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	// The inline default remains the prompt: the absent project layer is skipped.
	if got := systemMessage(ctrl.History()); !strings.Contains(got, "INLINE FALLBACK") {
		t.Fatalf("system prompt did not use inline fallback:\n%s", got)
	}
}

func TestBuildMissingUserSystemPromptFileWarnsAndFallsBack(t *testing.T) {
	isolateConfigHome(t)
	userHome := robustTempDir(t)
	t.Setenv("REASONIX_HOME", userHome)
	writeFile(t, userHome, "config.toml", systemPromptFileTestConfig("prompts/missing.md"))
	root := robustTempDir(t) // no project reasonix.toml

	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: root,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if got := systemMessage(ctrl.History()); !strings.Contains(got, "INLINE FALLBACK") {
		t.Fatalf("system prompt did not use inline fallback:\n%s", got)
	}
	for _, notice := range notices {
		if notice.Level == event.LevelWarn && strings.Contains(notice.Text, "falling back to inline/default system prompt") {
			return
		}
	}
	t.Fatalf("missing fallback warning: %#v", notices)
}

func TestBuildNonMissingSystemPromptReadFailureStaysFatal(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Setenv("REASONIX_HOME", robustTempDir(t))
	writeFile(t, root, "reasonix.toml", systemPromptFileTestConfig("prompts"))
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root})
	if ctrl != nil {
		ctrl.Close()
		t.Fatal("Build returned a controller after system prompt read failure")
	}
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("Build error = %v, want fatal system prompt read error", err)
	}
}

func systemPromptFileTestConfig(path string) string {
	return `
default_model = "test-model"

[agent]
system_prompt = "INLINE FALLBACK"
system_prompt_file = "` + path + `"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
}
