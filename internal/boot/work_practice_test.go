package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestBuildAppendsWorkPracticePolicyToCustomSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	for _, want := range []string{"Work practices:", "implement exactly that", "review the final diff"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("work practice policy missing %q from custom system prompt:\n%s", want, sys)
		}
	}
	if strings.Index(sys, config.UserDecisionPolicy) >= strings.Index(sys, config.WorkPracticePolicy) ||
		strings.Index(sys, config.WorkPracticePolicy) >= strings.Index(sys, config.LanguagePolicy) {
		t.Fatal("policy order must be user-decision < work-practice < language")
	}
}

func TestWorkPracticePolicyCarriesNoEnvironmentSpecificClaims(t *testing.T) {
	for _, unwanted := range []string{"offline", "proxy", "stop retrying", "avoid repeated full-suite runs", "pre-exist"} {
		if strings.Contains(strings.ToLower(config.WorkPracticePolicy), unwanted) {
			t.Errorf("work practice policy must not assert %q", unwanted)
		}
	}
}

func TestBuildInjectsOfflineNoteOnlyWhenEnvironmentDeclaresIt(t *testing.T) {
	build := func(t *testing.T, environmentSection string) string {
		t.Helper()
		dir := robustTempDir(t)
		t.Chdir(dir)
		writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`+environmentSection)

		ctrl, err := Build(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		defer ctrl.Close()
		return systemMessage(ctrl.History())
	}

	if sys := build(t, ""); strings.Contains(sys, config.OfflineEnvironmentNote) {
		t.Fatalf("undeclared environment includes offline note:\n%s", sys)
	}
	if sys := build(t, "\n[environment]\noffline = true\n"); !strings.Contains(sys, config.OfflineEnvironmentNote) {
		t.Fatalf("declared offline environment missing note:\n%s", sys)
	}
	sys := build(t, "\n[environment]\nenabled = false\noffline = true\n")
	if !strings.Contains(sys, config.OfflineEnvironmentNote) {
		t.Fatal("offline declaration must survive disabled probing")
	}
	if strings.Contains(sys, "## Environment") {
		t.Fatal("disabled probing must suppress the environment section")
	}
}
