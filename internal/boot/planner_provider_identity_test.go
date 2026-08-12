package boot

import (
	"context"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestBuildEnablesPlannerWhenProvidersShareModelName(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("shared-model-planner"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "planner"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "shared-model"

[[providers]]
name = "planner"
kind = "boot-token-profile-test"
model = "shared-model"
`)

	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if got := ctrl.Label(); got != "shared-model + planner shared-model" {
		t.Fatalf("controller label = %q, want planner enabled for distinct provider/model refs", got)
	}
}
