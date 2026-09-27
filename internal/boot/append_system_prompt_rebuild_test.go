package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/extension"
	"reasonix/internal/provider"
)

func TestBuildExternalSystemPromptRefreshesReusedAssembly(t *testing.T) {
	_, opts := externalPromptFixture(t)
	first := buildExternalPrompt(t, opts)
	opts.ReuseAssembly = first.Assembly
	opts.PreviousPlan = &extension.RuntimePlan{Kind: extension.SubgraphNone}
	writeExternalPrompt(t, opts.AppendSystemPromptFile, "UPDATED-HOST")
	second := buildExternalPrompt(t, opts)
	assertExternalPromptRefreshed(t, second, "UPDATED-HOST", "PRIVATE-CONTENT")
	opts.ReuseAssembly = second.Assembly
	opts.AppendSystemPromptFile = ""
	third := buildExternalPrompt(t, opts)
	if strings.Contains(systemMessage(third.Controller.History()), "UPDATED-HOST") {
		t.Fatal("removing the process option retained the previous external prompt")
	}
}

func TestRebuildExternalSystemPrompt(t *testing.T) {
	for _, name := range []string{"no-op graph", "reload", "model switch", "resumed session"} {
		t.Run(name, func(t *testing.T) {
			root, opts := externalPromptFixture(t)
			first := buildExternalPrompt(t, opts)
			if name == "model switch" {
				writeFile(t, root, "reasonix.toml", systemPromptFileTestConfig("")+`
[[providers]]
name = "other-model"
kind = "openai"
base_url = "https://example.invalid"
model = "other"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
				opts.Model = "other-model"
			}
			if name == "reload" {
				opts.ForceFullRebuild = true
			}
			if name == "resumed session" {
				resumeExternalPromptFixture(t, first, root)
			}
			writeExternalPrompt(t, opts.AppendSystemPromptFile, "UPDATED-HOST")
			second, err := RebuildFrom(context.Background(), first, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(second.Controller.Close)
			assertExternalPromptRefreshed(t, second, "UPDATED-HOST", "PRIVATE-CONTENT")
			if name == "model switch" && second.Controller.ModelRef() != "other-model/other" {
				t.Fatalf("external prompt rebuild selected model %q, want other-model/other", second.Controller.ModelRef())
			}
			if name == "resumed session" && second.Controller.SessionPath() != first.Controller.SessionPath() {
				t.Fatal("external prompt rebuild changed the resumed session identity")
			}
		})
	}
}

func TestRebuildExternalSystemPromptRevalidatesFile(t *testing.T) {
	for _, name := range []string{"missing", "empty", "invalid UTF-8"} {
		t.Run(name, func(t *testing.T) {
			_, opts := externalPromptFixture(t)
			first := buildExternalPrompt(t, opts)
			oldPrompt := systemMessage(first.Controller.History())
			switch name {
			case "missing":
				if err := os.Remove(opts.AppendSystemPromptFile); err != nil {
					t.Fatal(err)
				}
			case "empty":
				writeExternalPrompt(t, opts.AppendSystemPromptFile, "")
			case "invalid UTF-8":
				writeExternalPrompt(t, opts.AppendSystemPromptFile, "PRIVATE-CONTENT\xff")
			}
			for _, build := range []func() (*BuildResult, error){
				func() (*BuildResult, error) { return BuildRuntime(context.Background(), opts) },
				func() (*BuildResult, error) { return RebuildFrom(context.Background(), first, opts) },
			} {
				res, err := build()
				if err == nil {
					res.Controller.Close()
					t.Fatal("build accepted an invalid external prompt after the initial build")
				}
				assertExternalPromptErrorPrivate(t, err, opts.AppendSystemPromptFile)
			}
			if systemMessage(first.Controller.History()) != oldPrompt {
				t.Fatal("failed rebuild changed the active controller prompt")
			}
		})
	}
}

func TestExternalSystemPromptKeepsExtensionReplacementFinal(t *testing.T) {
	_, opts := externalPromptFixture(t)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "prompt-owner", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "EXTENSION PROMPT"},
	})
	first := buildExternalPrompt(t, opts)
	if got := first.Snapshot.SystemPrompt(); got != "EXTENSION PROMPT" {
		t.Fatalf("initial extension prompt = %q", got)
	}
	opts.ReuseAssembly = first.Assembly
	opts.PreviousPlan = &extension.RuntimePlan{Kind: extension.SubgraphNone}
	opts.PreviousDispatcher = first.Dispatcher
	writeExternalPrompt(t, opts.AppendSystemPromptFile, "UPDATED-HOST")
	second := buildExternalPrompt(t, opts)
	if got := second.Snapshot.SystemPrompt(); got != "EXTENSION PROMPT" {
		t.Fatalf("reused assembly bypassed extension replacement: %q", got)
	}
	if got := systemMessage(second.Controller.History()); got != "EXTENSION PROMPT" {
		t.Fatalf("controller bypassed extension replacement: %q", got)
	}
}

func resumeExternalPromptFixture(t *testing.T, res *BuildResult, root string) {
	t.Helper()
	path := filepath.Join(root, "selected-session.jsonl")
	s := agent.NewSession(systemMessage(res.Controller.History()))
	s.Add(provider.Message{Role: provider.RoleUser, Content: "original question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "original answer"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := res.Controller.ResumeNativeSession(s, path); err != nil {
		t.Fatal(err)
	}
}

func assertExternalPromptRefreshed(t *testing.T, res *BuildResult, current, stale string) {
	t.Helper()
	got := systemMessage(res.Controller.History())
	if strings.Count(got, current) != 1 || strings.Contains(got, stale) {
		t.Fatal("system prompt must contain the current external block once and omit the stale block")
	}
	if got != res.Snapshot.SystemPrompt() {
		t.Fatal("controller and snapshot system prompts differ after rebuild")
	}
}
