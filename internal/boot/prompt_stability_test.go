package boot

import (
	"context"
	"strings"
	"testing"
)

// TestBuildComposesByteStableSystemPrompt is the boot-level byte-stability
// guard: two Builds over the same workspace and config must compose the exact
// same system prompt. The system prompt is the provider-cached prefix of every
// request in every session — any byte of nondeterminism here (probe flaps,
// unsorted iteration, time-dependent content) cold-starts the provider cache
// for the whole machine, which is precisely the "desktop costs more" class
// (#2945). Environment probes are covered cross-process by the persisted
// snapshot tests in internal/environment; this test pins the rest of the
// composition (memory, skills index, output style, workspace line, policies).
func TestBuildComposesByteStableSystemPrompt(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
	writeFile(t, dir, "REASONIX.md", "Project rule: keep the prompt prefix stable.")

	first, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	firstPrompt := systemMessage(first.History())
	first.Close()
	if strings.TrimSpace(firstPrompt) == "" {
		t.Fatal("first Build composed an empty system prompt")
	}

	second, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	secondPrompt := systemMessage(second.History())
	second.Close()

	if firstPrompt != secondPrompt {
		t.Fatalf("system prompt is not byte-stable across identical Builds:\nfirst  (%d bytes)\nsecond (%d bytes)\nfirst diff site: %q",
			len(firstPrompt), len(secondPrompt), firstDivergence(firstPrompt, secondPrompt))
	}
}

// firstDivergence returns a small window around the first differing byte so a
// failure names the drifting prompt section instead of dumping both prompts.
func firstDivergence(a, b string) string {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	i := 0
	for i < limit && a[i] == b[i] {
		i++
	}
	start := i - 40
	if start < 0 {
		start = 0
	}
	endA := i + 40
	if endA > len(a) {
		endA = len(a)
	}
	endB := i + 40
	if endB > len(b) {
		endB = len(b)
	}
	return "..." + a[start:endA] + "... vs ..." + b[start:endB] + "..."
}
