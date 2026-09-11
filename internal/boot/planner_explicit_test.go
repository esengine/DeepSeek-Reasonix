package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestBuildOmitsCoordinatorWithoutPlannerModel(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("executor-only"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if strings.Contains(ctrl.Label(), "planner") {
		t.Fatalf("label = %q, want executor-only without planner_model", ctrl.Label())
	}
}

func TestBuildConstructsCoordinatorWhenPlannerModelConfigured(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("dual"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "planner"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"

[[providers]]
name = "planner"
kind = "boot-token-profile-test"
model = "planner-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if !strings.Contains(ctrl.Label(), "planner") {
		t.Fatalf("label = %q, want a coordinator when planner_model is set", ctrl.Label())
	}
}

func TestBuildConstructsCoordinatorWhenPlannerModelMatchesExecutor(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("same-model"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "executor"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "shared-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if !strings.Contains(ctrl.Label(), "planner") {
		t.Fatalf("label = %q, want a coordinator when planner_model is explicitly set even to the same model", ctrl.Label())
	}
}
