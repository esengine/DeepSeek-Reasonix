package agent

import (
	"context"
	"encoding/json"
	"errors"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/checkpoint"
)

func (a *Agent) applyTargetAccess(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a.svc.checkTargetAccess == nil {
		return toolOutcome{}, false
	}
	target, args := plan.executionTarget()
	if err := a.svc.checkTargetAccess(ctx, target, args); err != nil {
		msg := err.Error()
		var refusal tool.Refusal
		code := ""
		if errors.As(err, &refusal) {
			msg, code = refusal.Message, refusal.Code
		}
		return toolOutcome{output: msg, blocked: true, errMsg: msg, refusalCode: code}, true
	}
	return toolOutcome{}, false
}

func (a *Agent) preToolUse(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a.svc.hooks == nil {
		return toolOutcome{}, false
	}
	if !plan.readOnly && toolHooksMayMutateWorkspace(a.svc.hooks) && a.svc.mutationObserver != nil {
		a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{
			Reason: checkpoint.GapHookWrite, Tool: plan.evidenceName,
			Detail: "user tool hook writes are outside the tool preimage capture",
		})
	}
	// Resolved proxy calls use the concrete target name and arguments.
	if block, msg := a.svc.hooks.PreToolUse(ctx, plan.permName, plan.permArgs); block {
		if msg == "" {
			msg = "blocked by a PreToolUse hook"
		}
		return toolOutcome{output: "blocked: " + msg, blocked: true, errMsg: "blocked by PreToolUse hook"}, true
	}
	return toolOutcome{}, false
}

func (p *toolCallPlan) executionTarget() (tool.Tool, json.RawMessage) {
	if p.resolved.Target == nil {
		return p.execTool, p.execArgs
	}
	args := p.resolved.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return p.resolved.Target, args
}
