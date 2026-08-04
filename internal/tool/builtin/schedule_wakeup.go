package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(scheduleWakeup{}) }

// scheduleWakeup sets or clears the pending wakeup of a dynamic loop (one
// created with /loop and no fixed interval). The agent calls it after each
// iteration with the delay until the next check, or with stop:true when the
// loop's objective is complete.
type scheduleWakeup struct{}

func (scheduleWakeup) Name() string { return "schedule_wakeup" }

func (scheduleWakeup) Description() string {
	return "Schedule the next wakeup of a dynamic /loop. Call this after each loop iteration: pass `delay_minutes` (1-60) with a `reason` to check again later, or `stop: true` when the loop's objective is complete and it should end. Without this call, a dynamic loop stays paused after its current iteration."
}

func (scheduleWakeup) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "stop":{"type":"boolean","description":"Set true to cancel the pending wakeup and end the loop."},
  "delay_minutes":{"type":"number","description":"Minutes until the next wakeup (1-60). Required unless stop is true."},
  "reason":{"type":"string","description":"Brief reason for the chosen delay, e.g. \"CI still running, recheck shortly\"."}
},
"required":["stop"]
}`)
}

func (scheduleWakeup) ReadOnly() bool { return false }

func (scheduleWakeup) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sched, ok := scheduler.FromContext(ctx)
	if !ok || sched == nil {
		return "", fmt.Errorf("schedule_wakeup: no scheduler in this context")
	}
	var in struct {
		Stop         bool    `json:"stop"`
		DelayMinutes float64 `json:"delay_minutes"`
		Reason       string  `json:"reason"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("schedule_wakeup: %v", err)
	}
	if in.Stop {
		n := sched.StopWakeup()
		if n == 0 {
			return "no active dynamic loop to stop", nil
		}
		return fmt.Sprintf("loop stopped (%d pending wakeup(s) cleared)", n), nil
	}
	if in.DelayMinutes < 1 || in.DelayMinutes > 60 {
		return "", fmt.Errorf("schedule_wakeup: delay_minutes must be between 1 and 60, got %v", in.DelayMinutes)
	}
	delay := time.Duration(in.DelayMinutes * float64(time.Minute))
	n := sched.ScheduleWakeup(delay)
	if n == 0 {
		return "no dynamic loop to schedule — start one with /loop and a prompt", nil
	}
	reason := ""
	if in.Reason != "" {
		reason = " (" + in.Reason + ")"
	}
	return fmt.Sprintf("next wakeup in %.0f minute(s)%s — %d loop(s) scheduled", in.DelayMinutes, reason, n), nil
}
