package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/pty"
	"reasonix/internal/tool"
)

// classifyPTYToolCallPlan sets up PTY base permission, read-only resolution,
// and command-level secondary Bash permission for start and write_line.
// mgr is optional: when non-nil and action=="write", the session's pending-input
// buffer is consulted so split-across-calls commands are evaluated as a whole.
func classifyPTYToolCallPlan(plan *toolCallPlan, args json.RawMessage, mgr *pty.Manager) {
	if plan == nil || plan.canonicalName != "pty" {
		return
	}
	var p struct {
		Action    string `json:"action"`
		Command   string `json:"command"`
		Input     string `json:"input"`
		SessionID string `json:"session_id"`
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
		if mgr != nil {
			sessionID := strings.TrimSpace(p.SessionID)
			if sessionID == "" {
				sessionID = pty.DefaultSessionID
			}
			if sess, err := mgr.Get(sessionID); err == nil && sess.PendingInput() != "" {
				cmd = strings.TrimSpace(sess.PendingInput() + cmd)
			}
		}
		plan.commandPermName = "bash"
		plan.commandPermArgs, _ = json.Marshal(map[string]string{"command": cmd})
		plan.effects, _ = evidence.ClassifyBashToolCall(plan.commandPermArgs)
	case "write":
		// Multi-command support: when a write contains one or more newlines,
		// EVERY newline-terminated command line must be evaluated against the
		// Bash permission gate. If any command is denied, the entire write is blocked.
		// We read pending input without mutating the session (commit happens on
		// actual successful write in Session.Write).
		var combined string
		if mgr != nil {
			sessionID := strings.TrimSpace(p.SessionID)
			if sessionID == "" {
				sessionID = pty.DefaultSessionID
			}
			if sess, err := mgr.Get(sessionID); err == nil {
				combined = sess.PendingInput() + p.Input
			} else {
				combined = p.Input
			}
		} else {
			combined = p.Input
		}

		if strings.Contains(combined, "\n") {
			lines := strings.Split(combined, "\n")
			// All segments before the final \n are complete commands to be evaluated.
			for i := 0; i < len(lines)-1; i++ {
				cmd := strings.TrimSpace(lines[i])
				if cmd != "" {
					cmdArgs, _ := json.Marshal(map[string]string{"command": cmd})
					plan.commandPermCalls = append(plan.commandPermCalls, cmdArgs)
				}
			}
			if len(plan.commandPermCalls) > 0 {
				plan.commandPermName = "bash"
				plan.commandPermArgs = plan.commandPermCalls[0]
				var merged evidence.ToolEffects
				for i, callArgs := range plan.commandPermCalls {
					eff, _ := evidence.ClassifyBashToolCall(callArgs)
					if i == 0 {
						merged = eff
					} else {
						merged = mergeToolEffects(merged, eff)
					}
				}
				plan.effects = merged
			}
		}
	}
}

func mergeToolEffects(a, b evidence.ToolEffects) evidence.ToolEffects {
	return evidence.ToolEffects{
		StateMutation:      a.StateMutation || b.StateMutation,
		WorkspaceMutation:  a.WorkspaceMutation || b.WorkspaceMutation,
		ContentMutation:    a.ContentMutation || b.ContentMutation,
		RepositoryMutation: a.RepositoryMutation || b.RepositoryMutation,
		Known:              a.Known && b.Known,
		Reason:             strings.Trim(a.Reason+"; "+b.Reason, "; "),
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
	if allow {
		if len(plan.commandPermCalls) > 0 {
			for _, cmdArgs := range plan.commandPermCalls {
				cmdAllow, cmdReason, cmdErr := gate.Check(ctx, "bash", cmdArgs, plan.readOnly)
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
					break
				}
			}
		} else if plan.commandPermName != "" && len(plan.commandPermArgs) > 0 {
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
