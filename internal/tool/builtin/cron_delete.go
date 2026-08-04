package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(cronDelete{}) }

// cronDelete cancels a scheduled task by its 8-character ID (see cron_list).
// The agent calls it when the user asks to cancel a loop or reminder.
type cronDelete struct{}

func (cronDelete) Name() string { return "cron_delete" }

func (cronDelete) Description() string {
	return "Cancel a scheduled task by its ID (listed by cron_list). The task stops firing immediately. Use when asked to cancel a loop or reminder, or when a recurring task has outlived its purpose."
}

func (cronDelete) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "id":{"type":"string","description":"The 8-character task ID, e.g. \"a1b2c3d4\"."}
},
"required":["id"]
}`)
}

func (cronDelete) ReadOnly() bool { return false }

func (cronDelete) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sched, ok := scheduler.FromContext(ctx)
	if !ok || sched == nil {
		return "", fmt.Errorf("cron_delete: no scheduler in this context")
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("cron_delete: %v", err)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return "", fmt.Errorf("cron_delete: id is required")
	}
	if sched.Delete(id) {
		return fmt.Sprintf("cancelled scheduled task %s", id), nil
	}
	return fmt.Sprintf("no scheduled task with id %s — see cron_list for active tasks", id), nil
}
