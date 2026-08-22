package boot

import (
	"context"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

// Two providers can offer the same model name (deepseek vs a vendor gateway
// both serving deepseek-v4-flash). They are distinct models — separate
// pricing, context window, and cache prefix — so two-model collaboration must
// engage. Boot used to compare bare model names, collapsing this setup into
// "no planner" (#8230).
func TestBuildEnablesPlannerWhenProvidersShareModelName(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("deepseek-v4-flash"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek/deepseek-v4-flash"

[agent]
planner_model = "vendor-gw/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "boot-token-profile-test"
model = "deepseek-v4-flash"

[[providers]]
name = "vendor-gw"
kind = "boot-token-profile-test"
model = "deepseek-v4-flash"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.FuncSink(func(event.Event) {})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ctrl == nil {
		t.Fatal("Build returned no controller")
	}
	if !ctrl.DualModel() {
		t.Fatal("same model name under two providers must still enable the planner")
	}
}

// The identical single-provider case (planner ref equals the executor ref)
// stays single-model: a model planning with itself gains nothing and would
// only duplicate the session.
func TestBuildKeepsSingleModelWhenPlannerRefEqualsExecutor(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("deepseek-v4-flash"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek/deepseek-v4-flash"

[agent]
planner_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "boot-token-profile-test"
model = "deepseek-v4-flash"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.FuncSink(func(event.Event) {})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ctrl == nil {
		t.Fatal("Build returned no controller")
	}
	if ctrl.DualModel() {
		t.Fatal("planner ref identical to the executor ref must stay single-model")
	}
}
