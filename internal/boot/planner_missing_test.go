package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestBuildKeepsExecutorWhenPlannerModelIsUnresolvable(t *testing.T) {
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

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("an unusable optional planner must not block startup: %v", err)
	}
	if ctrl == nil {
		t.Fatal("Build returned no controller")
	}

	var warned bool
	for _, n := range notices {
		if strings.Contains(n, "planner_model") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("the dropped planner must be announced, got notices %v", notices)
	}
}
