package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// classifyPTYToolCallPlan sets up PTY base permission, read-only resolution,
// and command-level secondary Bash permission for start and write_line.
func classifyPTYToolCallPlan(plan *toolCallPlan, args json.RawMessage) {
	if plan == nil || plan.canonicalName != "pty" {
		return
	}
	var p struct {
		Action  string `json:"action"`
		Command string `json:"command"`
		Input   string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	switch action {
	case "read", "list":
		plan.readOnly = true
		plan.resolvedMeta = &tool.ResolvedCall{TargetName: plan.canonicalName, ReadOnly: true}
	case "start":
		cmd := strings.TrimSpace(p.Command)
		if cmd == "" {
			cmd = "bash"
		}
		plan.commandPermName = "bash"
		plan.commandPermArgs, _ = json.Marshal(map[string]string{"command": cmd})
	case "write_line":
		cmd := strings.TrimSpace(p.Command)
		if cmd == "" {
			cmd = strings.TrimSpace(p.Input)
		}
		plan.commandPermName = "bash"
		plan.commandPermArgs, _ = json.Marshal(map[string]string{"command": cmd})
		plan.effects, _ = evidence.ClassifyBashToolCall(plan.commandPermArgs)
	}
}

func (a *Agent) applyStandardGate(ctx context.Context, plan *toolCallPlan, gate Gate) (toolOutcome, bool) {
	allow, reason, err := gate.Check(ctx, plan.permName, plan.permArgs, plan.readOnly)
	if err != nil {
		return toolOutcome{
			output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
			blocked: true,
			errMsg:  fmt.Sprintf("blocked: %v", err),
		}, true
	}
	if allow && plan.commandPermName != "" && len(plan.commandPermArgs) > 0 {
		cmdAllow, cmdReason, cmdErr := gate.Check(ctx, plan.commandPermName, plan.commandPermArgs, plan.readOnly)
		if cmdErr != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", cmdReason, cmdErr),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", cmdErr),
			}, true
		}
		if !cmdAllow {
			allow = false
			reason = cmdReason
		}
	}
	if blocked, early := a.interceptExtensionPermission(ctx, plan, &allow); early {
		return blocked, true
	}
	if !allow {
		return toolOutcome{
			output:  "blocked: " + reason,
			blocked: true,
			errMsg:  "blocked by permission policy",
		}, true
	}
	return toolOutcome{}, false
}
