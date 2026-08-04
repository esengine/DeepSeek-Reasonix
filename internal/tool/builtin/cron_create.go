package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(cronCreate{}) }

// cronCreate schedules a new task (recurring cron or one-shot reminder). The
// agent calls it when the user asks for a scheduled job, e.g. "check the
// deploy every 5 minutes" or "remind me in 45 minutes". The 5-field cron
// expression may also be a human interval token (5m, 2h, 1d), which maps to
// the nearest clean cron step.
type cronCreate struct{}

func (cronCreate) Name() string { return "cron_create" }

func (cronCreate) Description() string {
	return "Create a scheduled task. Accepts `cron` — a 5-field cron expression like \"*/5 * * * *\" (or an interval token like \"5m\", \"2h\", \"1d\", rounded to the nearest clean step) — and `prompt`, the text to run when it fires. Set `one_shot: true` for a single fire that deletes itself (reminders). The task fires between turns while this session is open, at the next matching time; list tasks with cron_list, cancel with cron_delete. The session holds at most 50 tasks."
}

func (cronCreate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "cron":{"type":"string","description":"5-field cron expression (minute hour dom month dow), e.g. \"*/5 * * * *\", or an interval token like \"5m\"/\"2h\"/\"1d\"."},
  "prompt":{"type":"string","description":"The prompt to run when the task fires."},
  "one_shot":{"type":"boolean","description":"Set true for a reminder that fires once and deletes itself."}
},
"required":["cron","prompt"]
}`)
}

func (cronCreate) ReadOnly() bool { return false }

func (cronCreate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sched, ok := scheduler.FromContext(ctx)
	if !ok || sched == nil {
		return "", fmt.Errorf("cron_create: no scheduler in this context")
	}
	var in struct {
		Cron    string `json:"cron"`
		Prompt  string `json:"prompt"`
		OneShot bool   `json:"one_shot"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("cron_create: %v", err)
	}
	cron := strings.TrimSpace(in.Cron)
	prompt := strings.TrimSpace(in.Prompt)
	if cron == "" || prompt == "" {
		return "", fmt.Errorf("cron_create: cron and prompt are required")
	}
	if interval, ok := scheduler.ParseInterval(cron); ok {
		cron = interval
	} else if !scheduler.Valid(cron) {
		return "", fmt.Errorf("cron_create: %q is neither a 5-field cron expression nor a valid interval token (s/m/h/d)", in.Cron)
	}
	id, err := sched.Add(cron, prompt, time.Now(), in.OneShot)
	if err != nil {
		return "", fmt.Errorf("cron_create: %v", err)
	}
	kind := "recurring"
	if in.OneShot {
		kind = "one-shot"
	}
	views := sched.Tasks()
	next := ""
	for _, v := range views {
		if v.ID == id {
			next = v.NextFire
			break
		}
	}
	if next == "" {
		next = "as soon as the session is idle"
	}
	return fmt.Sprintf("created %s task %s — schedule %s, next fire %s\nprompt: %s", kind, id, cron, next, promptPreviewForTool(prompt)), nil
}

// promptPreviewForTool shortens a prompt for confirmation text.
func promptPreviewForTool(prompt string) string {
	p := strings.Join(strings.Fields(prompt), " ")
	if len(p) > 120 {
		return p[:117] + "..."
	}
	return p
}
