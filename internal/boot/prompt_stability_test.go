package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
)

func sessionContextMessage(msgs []provider.Message) string {
	for _, message := range msgs {
		if sessioncontext.IsContent(message.Content) {
			return message.Content
		}
	}
	return ""
}

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

func TestBackgroundMemoryAndSkillCatalogChangesDoNotChangeSystemPrompt(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "STABLE BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	buildSystem := func() (*memory.Set, string) {
		ctrl, err := Build(context.Background(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		return ctrl.Memory(), systemMessage(ctrl.History())
	}
	mem, baseline := buildSystem()
	if _, err := mem.Store.Save(memory.Memory{
		Name: "dynamic-cache-fact", Description: "background-only fact",
		Activation: memory.ActivationRelevant, Body: "secret body",
	}); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(dir, ".reasonix", "skills", "dynamic-skill", "SKILL.md")
	writeFile(t, dir, ".reasonix/skills/dynamic-skill/SKILL.md", "---\ndescription: dynamic catalog entry\n---\nbody")

	_, afterAdd := buildSystem()
	if afterAdd != baseline {
		t.Fatalf("background memory/skill addition changed system prompt: %q", firstDivergence(baseline, afterAdd))
	}
	if strings.Contains(afterAdd, "dynamic-cache-fact") || strings.Contains(afterAdd, "dynamic-skill") || strings.Contains(afterAdd, "secret body") {
		t.Fatalf("dynamic catalog data leaked into system:\n%s", afterAdd)
	}
	if err := os.Remove(skillPath); err != nil {
		t.Fatal(err)
	}
	if err := mem.Store.Delete("dynamic-cache-fact"); err != nil {
		t.Fatal(err)
	}
	_, afterDelete := buildSystem()
	if afterDelete != baseline {
		t.Fatalf("background memory/skill deletion changed system prompt: %q", firstDivergence(baseline, afterDelete))
	}

	writeFile(t, dir, "AGENTS.md", "Standing rule: preserve the public API.")
	_, withStanding := buildSystem()
	if withStanding == baseline || !strings.Contains(withStanding, "Standing rule: preserve the public API.") {
		t.Fatalf("standing instruction did not intentionally change system:\n%s", withStanding)
	}
}

func TestDisableImplicitSkillInvocationOmitsPolicyAndCatalogButKeepsSlashSkill(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("implicit-off", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[skills]
disable_implicit_invocation = true

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	writeFile(t, dir, ".reasonix/skills/hot/SKILL.md", "---\ndescription: explicit hot skill\n---\nHOT BODY")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if sys := systemMessage(ctrl.History()); strings.Contains(sys, "# Skills") || strings.Contains(sys, "explicit hot skill") {
		t.Fatalf("implicit-off system contains skill policy/catalog:\n%s", sys)
	}
	if rendered, ok := ctrl.RunSkill("/hot now"); !ok || !strings.Contains(rendered, "HOT BODY") {
		t.Fatalf("explicit slash skill = %q, %v", rendered, ok)
	}
	_ = ctrl.Run(context.Background(), "capture request prefix")
	if req := prov.LastRequest(); req == nil || strings.Contains(sessionContextMessage(req.Messages), "explicit hot skill") {
		t.Fatalf("implicit-off request unexpectedly contains skills catalog: %+v", req)
	}
}

// firstDivergence returns a small window around the first differing byte so a
// failure names the drifting prompt section instead of dumping both prompts.
func firstDivergence(a, b string) string {
	limit := min(len(b), len(a))
	i := 0
	for i < limit && a[i] == b[i] {
		i++
	}
	start := max(i-40, 0)
	endA := min(i+40, len(a))
	endB := min(i+40, len(b))
	return "..." + a[start:endA] + "... vs ..." + b[start:endB] + "..."
}
