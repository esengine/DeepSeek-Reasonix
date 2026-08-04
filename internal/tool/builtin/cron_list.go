package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(cronList{}) }

// cronList reports the session's scheduled tasks (fixed /loop schedules,
// dynamic loops, and one-shot reminders) so the agent can answer "what
// scheduled tasks do I have?".
type cronList struct{}

func (cronList) Name() string { return "cron_list" }

func (cronList) Description() string {
	return "List all scheduled tasks in the session: their IDs, schedules (cron expressions or dynamic), next fire times, and prompts. Use it when asked what scheduled tasks exist, or before cancelling one."
}

func (cronList) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

func (cronList) ReadOnly() bool { return true }

func (cronList) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sched, ok := scheduler.FromContext(ctx)
	if !ok || sched == nil {
		return "no scheduled tasks — the scheduler is not available in this context", nil
	}
	views := sched.Tasks()
	if len(views) == 0 {
		return "no scheduled tasks", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d scheduled task(s):\n", len(views))
	for _, v := range views {
		schedule := "dynamic"
		if v.CronExpr != "" {
			schedule = v.CronExpr
		}
		next := v.NextFire
		if next == "" {
			next = "paused"
		}
		prompt := v.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}
		oneShot := ""
		if v.OneShot {
			oneShot = " (one-shot)"
		}
		noExpire := ""
		if v.NoExpire {
			noExpire = " (no expiry)"
		}
		fmt.Fprintf(&b, "  %s  %-14s  next %s%s%s  %q\n", v.ID, schedule, next, oneShot, noExpire, prompt)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
