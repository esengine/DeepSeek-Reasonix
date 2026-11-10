package agent

import (
	"context"
	"encoding/json"
	"errors"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/checkpoint"
)

func (a *Agent) applyTargetAccess(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	check := a.svc.checkTargetAccess
	if check == nil {
		check = tool.UnconfiguredFileAccess
	}
	target, args := plan.executionTarget()
	if err := check(a.withWriteApprover(ctx), target, args); err != nil {
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
	if toolHooksMayMutateWorkspace(a.svc.hooks) {
		plan.leasePreparation.Retain()
	}
	if toolHooksMayMutateWorkspace(a.svc.hooks) && a.svc.mutationObserver != nil {
		a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{
			Reason: checkpoint.GapHookWrite, Tool: plan.evidenceName,
			Detail: "user tool hook writes are outside the tool preimage capture",
		})
	}
	// Resolved proxy calls use the concrete target name and arguments.
	plan.hookMayHaveMutated = toolHooksMayMutateWorkspace(a.svc.hooks)
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

// withWriteApprover lets the admission check see whether a person can be asked
// about a write outside the scope. The approver is stamped on a call's own
// context only after admission, so the check would otherwise refuse what
// execution could have put to the user.
func (a *Agent) withWriteApprover(ctx context.Context) context.Context {
	return tool.WithConfigWriteApprover(ctx, a.svc.configWrite)
}
