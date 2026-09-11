package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestBuildFailsWhenGuardianModelIsUnresolvable(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("guardian-missing"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
guardian_model = "missing-guardian"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err == nil || !strings.Contains(err.Error(), "guardian_model") {
		t.Fatalf("Build = (%v, %v), want a guardian_model configuration error", ctrl, err)
	}
}

func TestBuildFailsWhenRecoveryModelIsUnresolvable(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("recovery-missing"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
recovery_model = "missing-recovery"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err == nil || !strings.Contains(err.Error(), "recovery_model") {
		t.Fatalf("Build = (%v, %v), want a recovery_model configuration error", ctrl, err)
	}
}

func TestBuildLeavesOptionalRolesOffWithoutExplicitModels(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("roles-off"))

	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[[providers]]
name = "executor"
kind = "boot-token-profile-test"
model = "executor-model"
`)

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	for _, n := range notices {
		if strings.Contains(strings.ToLower(n), "guardian enabled") {
			t.Fatalf("guardian started without guardian_model: %v", notices)
		}
	}
	if strings.Contains(ctrl.Label(), "planner") {
		t.Fatalf("planner started without planner_model: %q", ctrl.Label())
	}
}
