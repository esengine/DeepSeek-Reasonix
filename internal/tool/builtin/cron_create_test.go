package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/scheduler"
)

func cronCreateCtx() context.Context {
	return scheduler.NewContext(context.Background(), scheduler.New())
}

// cronCreateCall runs the tool with args (parens required: composite literal + method call).
func cronCreateCall(ctx context.Context, args json.RawMessage) (string, error) {
	return (cronCreate{}).Execute(ctx, args)
}

func TestCronCreate(t *testing.T) {
	ctx := cronCreateCtx()
	args, _ := json.Marshal(map[string]any{
		"cron":   "*/5 * * * *",
		"prompt": "check the deploy",
	})
	out, err := cronCreateCall(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "check the deploy") || !strings.Contains(out, "*/5 * * * *") {
		t.Errorf("confirmation missing details: %q", out)
	}
	sched, _ := scheduler.FromContext(ctx)
	views := sched.Tasks()
	if len(views) != 1 {
		t.Fatalf("tasks = %d, want 1", len(views))
	}
	if views[0].CronExpr != "*/5 * * * *" || views[0].Prompt != "check the deploy" || views[0].OneShot {
		t.Errorf("task = %+v, want cron */5 prompt check the deploy, not one-shot", views[0])
	}
}

func TestCronCreateIntervalToken(t *testing.T) {
	ctx := cronCreateCtx()
	args, _ := json.Marshal(map[string]any{
		"cron":   "5m",
		"prompt": "poll ci",
	})
	if _, err := cronCreateCall(ctx, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	sched, _ := scheduler.FromContext(ctx)
	views := sched.Tasks()
	if len(views) != 1 || views[0].CronExpr != "*/5 * * * *" {
		t.Fatalf("interval token 5m not expanded: %+v", views)
	}
}

func TestCronCreateOneShotReminder(t *testing.T) {
	ctx := cronCreateCtx()
	args, _ := json.Marshal(map[string]any{
		"cron":     "*/1 * * * *",
		"prompt":   "push the release branch",
		"one_shot": true,
	})
	out, err := cronCreateCall(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "one-shot") {
		t.Errorf("confirmation missing one-shot kind: %q", out)
	}
	sched, _ := scheduler.FromContext(ctx)
	if views := sched.Tasks(); len(views) != 1 || !views[0].OneShot {
		t.Fatalf("task not one-shot: %+v", views)
	}
}

func TestCronCreateNoExpire(t *testing.T) {
	ctx := cronCreateCtx()
	args, _ := json.Marshal(map[string]any{
		"cron":      "*/5 * * * *",
		"prompt":    "endless deploy watch",
		"no_expire": true,
	})
	out, err := cronCreateCall(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no expiry") {
		t.Errorf("confirmation missing no-expiry note: %q", out)
	}
	sched, _ := scheduler.FromContext(ctx)
	views := sched.Tasks()
	if len(views) != 1 || !views[0].NoExpire {
		t.Fatalf("task not marked NoExpire: %+v", views)
	}
}

func TestCronCreateErrors(t *testing.T) {
	ctx := cronCreateCtx()
	for _, tc := range []struct {
		name string
		in   map[string]any
	}{
		{"missing prompt", map[string]any{"cron": "*/5 * * * *"}},
		{"missing cron", map[string]any{"prompt": "x"}},
		{"empty args", map[string]any{}},
		{"bad cron", map[string]any{"cron": "not a cron", "prompt": "x"}},
		{"out of range cron", map[string]any{"cron": "60 * * * *", "prompt": "x"}},
	} {
		args, _ := json.Marshal(tc.in)
		if _, err := cronCreateCall(ctx, args); err == nil {
			t.Errorf("%s: expected error, got none", tc.name)
		}
	}
}

func TestCronCreateTaskLimit(t *testing.T) {
	sched := scheduler.New()
	ctx := scheduler.NewContext(context.Background(), sched)
	for i := 0; i < scheduler.DefaultTaskLimit; i++ {
		args, _ := json.Marshal(map[string]any{"cron": "*/5 * * * *", "prompt": "x"})
		if _, err := cronCreateCall(ctx, args); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	args, _ := json.Marshal(map[string]any{"cron": "*/5 * * * *", "prompt": "over"})
	if _, err := cronCreateCall(ctx, args); err == nil {
		t.Error("expected task-limit error, got none")
	}
}
