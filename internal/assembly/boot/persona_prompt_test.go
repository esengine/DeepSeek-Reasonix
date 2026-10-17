package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

// personaConfigTemplate is the smallest workspace a Build needs, with %s taking
// an extra block placed after the [agent] scalars: a [agent.persona] table, or
// an [environment] table for the runtime-context tests.
const personaConfigTemplate = `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"
%s

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`

// buildPersonaPrompt builds one runtime over a fresh workspace with the given
// extra config block and returns the composed system prompt.
func buildPersonaPrompt(t *testing.T, extra string) string {
	t.Helper()
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("REASONIX_STATE_HOME", "")
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(personaConfigTemplate, extra))
	approveWorkspace(t, dir)
	b, err := Build(context.Background(), Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	prompt := systemMessage(b.History())
	b.Close()
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("Build composed an empty system prompt")
	}
	return prompt
}

func lastPolicyIndex(prompt string) int {
	return strings.LastIndex(prompt, config.ExternalContentPolicy)
}

func TestPersonaPrefixLeadsAndSuffixFollowsPolicies(t *testing.T) {
	prompt := buildPersonaPrompt(t, `
[agent.persona]
prefix = "PERSONA IDENTITY"
suffix = "PERSONA SUFFIX"
`)
	if !strings.HasPrefix(prompt, "PERSONA IDENTITY") {
		t.Fatalf("the persona prefix does not lead the prompt: %q", prompt[:min(len(prompt), 120)])
	}
	base := strings.Index(prompt, "BASE SYSTEM PROMPT")
	prefix := strings.Index(prompt, "PERSONA IDENTITY")
	suffix := strings.Index(prompt, "PERSONA SUFFIX")
	if !(prefix < base && base < suffix) {
		t.Fatalf("order prefix/base/suffix = %d/%d/%d, want prefix before base before suffix", prefix, base, suffix)
	}
	if last := lastPolicyIndex(prompt); suffix < last {
		t.Fatalf("persona suffix at %d is before the last core policy at %d", suffix, last)
	}
	if env := strings.Index(prompt, "## Environment"); env >= 0 && suffix > env {
		t.Fatalf("persona suffix at %d follows the environment section at %d", suffix, env)
	}
}

func TestPersonaCompleteIsTheEntirePrompt(t *testing.T) {
	prompt := buildPersonaPrompt(t, `
[agent.persona]
prefix = "ONLY THIS PERSONA"
suffix = "MUST NOT APPEAR"
complete = true
`)
	if prompt != "ONLY THIS PERSONA" {
		t.Fatalf("complete mode prompt = %q, want exactly the prefix", prompt)
	}
	if strings.Contains(prompt, "BASE SYSTEM PROMPT") || strings.Contains(prompt, config.WorkPracticePolicy) {
		t.Fatalf("complete mode kept other prompt sections: %q", prompt)
	}
}

func TestPersonaRuntimeContextSwitch(t *testing.T) {
	const offline = "\n[environment]\noffline = true\n"
	for _, tc := range []struct {
		name     string
		persona  string
		wantEnv  bool
		wantNote bool
	}{
		{
			name: "default includes runtime context",
			persona: `
[agent.persona]
prefix = "IDENTITY"
`,
			wantEnv: true, wantNote: true,
		},
		{
			name: "false drops the environment section and offline note",
			persona: `
[agent.persona]
prefix = "IDENTITY"
suffix = "SUFFIX"
include_runtime_context = false
`,
		},
		{
			name: "the switch alone is enough",
			persona: `
[agent.persona]
include_runtime_context = false
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt := buildPersonaPrompt(t, tc.persona+offline)
			if got := strings.Contains(prompt, "## Environment"); got != tc.wantEnv {
				t.Fatalf("environment section present = %v, want %v:\n%s", got, tc.wantEnv, prompt)
			}
			if got := strings.Contains(prompt, config.OfflineEnvironmentNote); got != tc.wantNote {
				t.Fatalf("offline note present = %v, want %v", got, tc.wantNote)
			}
			// Instructions and memory/skill guidance are never optional context.
			if !strings.Contains(prompt, "BASE SYSTEM PROMPT") || !strings.Contains(prompt, config.WorkPracticePolicy) {
				t.Fatalf("runtime-context suppression removed instructions or policies:\n%s", prompt)
			}
		})
	}
}

// An empty persona must not move a single byte: that is the compatibility
// promise for every config written before this feature existed.
func TestEmptyPersonaLeavesThePromptUnchanged(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("REASONIX_STATE_HOME", "")
	dir := robustTempDir(t)
	t.Chdir(dir)
	approveWorkspace(t, dir)
	opts := Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell()}
	build := func(extra string) string {
		writeFile(t, dir, "reasonix.toml", fmt.Sprintf(personaConfigTemplate, extra))
		b, err := Build(context.Background(), opts)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		prompt := systemMessage(b.History())
		b.Close()
		return prompt
	}
	without := build("")
	for name, extra := range map[string]string{
		"empty table":  "\n[agent.persona]\n",
		"blank bodies": "\n[agent.persona]\nprefix = \"   \"\nsuffix = \"\"\n",
	} {
		if got := build(extra); got != without {
			t.Fatalf("%s changed the prompt:\nwithout: %q\nwith:    %q", name, without, got)
		}
	}
}
