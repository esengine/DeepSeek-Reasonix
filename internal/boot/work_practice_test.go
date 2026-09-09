package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
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
	build := func(t *testing.T, environmentSection string) (string, string) {
		t.Helper()
		dir := robustTempDir(t)
		t.Chdir(dir)
		registerBootTokenProfileTestProvider()
		prov := testutil.NewMock("offline-context", testutil.Turn{Text: "done"})
		setBootTokenProfileTestProvider(t, prov)
		writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`+environmentSection)

		ctrl, err := Build(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		defer ctrl.Close()
		_ = ctrl.Run(context.Background(), "capture request prefix")
		if prov.LastRequest() == nil {
			t.Fatal("provider received no request")
		}
		return systemMessage(ctrl.History()), sessionContextMessage(ctrl.History())
	}

	if sys, session := build(t, ""); strings.Contains(sys, config.OfflineEnvironmentNote) || strings.Contains(session, config.OfflineEnvironmentNote) {
		t.Fatalf("undeclared environment includes offline note:\nsystem=%s\ncontext=%s", sys, session)
	}
	if sys, session := build(t, "\n[environment]\noffline = true\n"); strings.Contains(sys, config.OfflineEnvironmentNote) || !strings.Contains(session, config.OfflineEnvironmentNote) {
		t.Fatalf("declared offline note must live only in session context:\nsystem=%s\ncontext=%s", sys, session)
	}
	sys, session := build(t, "\n[environment]\nenabled = false\noffline = true\n")
	if !strings.Contains(session, config.OfflineEnvironmentNote) {
		t.Fatal("offline declaration must survive disabled probing")
	}
	if strings.Contains(sys, config.OfflineEnvironmentNote) || strings.Contains(session, "- OS:") {
		t.Fatal("disabled probing must suppress probed environment details and keep offline note out of system")
	}
}
