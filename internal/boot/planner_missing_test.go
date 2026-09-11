package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestBuildFailsWhenPlannerModelIsUnresolvable(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("planner-missing"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "OpenRouter/moonshotai/kimi-k2.6:free"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err == nil || !strings.Contains(err.Error(), "planner_model") {
		t.Fatalf("Build = (%v, %v), want a planner_model configuration error", ctrl, err)
	}
	if ctrl != nil {
		t.Fatal("Build must not return a controller when planner_model is unusable")
	}
}
