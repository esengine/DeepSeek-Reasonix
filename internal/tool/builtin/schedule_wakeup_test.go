package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/scheduler"
)

func scheduleWakeupCtx() context.Context {
	return scheduler.NewContext(context.Background(), scheduler.New())
}

// TestScheduleWakeupRejectsNonFiniteDelay covers the security-review follow-up:
// ordinary bounds comparisons let NaN and Inf through (every comparison with
// NaN is false, and the JSON number 1e999 overflows float64 to +Inf), which
// would hand time.Duration a non-finite value. NaN itself is not expressible
// in strict JSON, so the Inf vector (1e999 / -1e999) plus the plain bounds are
// the reachable cases.
func TestScheduleWakeupRejectsNonFiniteDelay(t *testing.T) {
	ctx := scheduleWakeupCtx()
	cases := []struct {
		name string
		args string
	}{
		{"positive infinity", `{"delay_minutes": 1e999}`},
		{"negative infinity", `{"delay_minutes": -1e999}`},
		{"zero", `{"delay_minutes": 0}`},
		{"above bound", `{"delay_minutes": 61}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := (scheduleWakeup{}).Execute(ctx, json.RawMessage(c.args))
			if err == nil || !strings.Contains(err.Error(), "delay_minutes") {
				t.Fatalf("err = %v, want a delay_minutes bounds error", err)
			}
		})
	}
}

func TestScheduleWakeupAcceptsValidDelay(t *testing.T) {
	ctx := scheduleWakeupCtx()
	sched, _ := scheduler.FromContext(ctx)
	if _, err := sched.Add("", "watch the deploy", time.Now(), false, false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	out, err := (scheduleWakeup{}).Execute(ctx, json.RawMessage(`{"delay_minutes": 5, "reason": "check later"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "5") {
		t.Errorf("confirmation missing delay: %q", out)
	}
	if !sched.HasPendingDynamic() {
		t.Error("valid delay did not arm the wakeup")
	}
}
