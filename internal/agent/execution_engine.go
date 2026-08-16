package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/taskcontract"
	"reasonix/internal/tool"
)

func mergeInheritedConstraints(child, parent runtimepolicy.Constraints) runtimepolicy.Constraints {
	if parent.ForbidMutation {
		child.ForbidMutation = true
	}
	if parent.ForbidTests {
		child.ForbidTests = true
	}
	if parent.ForbidExternal {
		child.ForbidExternal = true
	}
	if parent.PlanModeReadOnly {
		child.PlanModeReadOnly = true
		child.ForbidMutation = true
	}
	if parent.RequireFullVerification {
		child.RequireFullVerification = true
	}
	if len(parent.AllowedChecks) > 0 && len(child.AllowedChecks) == 0 {
		child.AllowedChecks = append([]string(nil), parent.AllowedChecks...)
	}
	return child
}

func (a *Agent) rebuildTurnContract() {
	if a == nil || a.turn.engine == nil {
		return
	}
	var plan *taskcontract.PlanFacts
	if snapshot := a.planContractSnapshot(); snapshot != nil {
		facts := planFacts(*snapshot)
		plan = &facts
	}
	var todos []evidence.TodoItem
	if a.task.ledger != nil {
		if items, ok := a.task.ledger.LatestTodos(); ok {
			todos = items
		}
	}
	var checks []string
	for _, check := range a.projectChecks {
		if command := strings.TrimSpace(check.Command); command != "" {
			checks = append(checks, command)
		}
	}
	var receipts []evidence.Receipt
	if a.task.ledger != nil {
		receipts = a.task.ledger.Receipts()
	}
	a.turn.engine.Rebuild(taskcontract.RebuildFacts{
		Plan:                    plan,
		Todos:                   todos,
		ProjectChecks:           checks,
		Receipts:                receipts,
		TestsForbidden:          a.turn.constraints.ForbidTests,
		RequireFullVerification: a.turn.constraints.RequireFullVerification,
		WorkspaceRoot:           a.writeWorkspaceRoot,
		HasApprovedPlan:         plan != nil,
		HasActiveGoal:           a.turn.deliveryScopeActive,
	})
}

func (a *Agent) pipelineDecision(plan *toolCallPlan) runtimepolicy.GuardDecision {
	if a == nil || plan == nil || a.turn.engine == nil {
		return runtimepolicy.GuardDecision{}
	}
	profile := evidence.ClassifyEffect(evidence.EffectInput{
		ToolName:       plan.evidenceName,
		Args:           plan.evidenceArgs,
		StaticReadOnly: plan.readOnly,
		Hint:           effectHintOf(plan.execTool, plan.execArgs),
		ActualPaths:    evidence.ToolCallPaths(plan.evidenceArgs),
	})
	plan.profile = profile
	plan.effects = profile.ToolEffects()
	return a.turn.engine.BeforeTool(runtimepolicy.CallContext{
		ToolName:       plan.evidenceName,
		Args:           plan.evidenceArgs,
		Profile:        profile,
		PlanReadOnly:   a.planMode.Load() || a.turn.constraints.PlanModeReadOnly,
		Interactive:    a.hasInteractiveAsk(),
		HasTodo:        a.hasActiveCanonicalTodo() || a.turn.deliveryCriteriaEstablished,
		HasCriteria:    a.turn.deliveryCriteriaEstablished,
		Verification:   plan.evidenceName == "bash" && evidence.IsVerificationCommand(bashCommandFromArgs(plan.evidenceArgs)),
		TestsForbidden: a.turn.constraints.ForbidTests,
		WorkspaceRoot:  a.writeWorkspaceRoot,
	})
}

func (a *Agent) commitToolReceipt(rec evidence.Receipt) {
	if a == nil || a.turn.engine == nil {
		return
	}
	a.turn.engine.CommitReceipt(runtimepolicy.ResultContext{
		Receipt:        rec,
		Profile:        evidence.ClassifyEffect(evidence.EffectInput{ToolName: rec.ToolName, Args: rec.Args, ActualPaths: rec.Paths, StaticReadOnly: rec.Read && !rec.Write}),
		WorkspaceRoot:  a.writeWorkspaceRoot,
		TestsForbidden: a.turn.constraints.ForbidTests,
	})
}

func effectHintOf(t tool.Tool, args json.RawMessage) evidence.CallHint {
	if t == nil {
		return evidence.CallHint{}
	}
	hint := evidence.CallHint{Present: true, ReadOnly: t.ReadOnly()}
	if d, ok := t.(interface{ MCPDestructiveHint() bool }); ok {
		hint.Destructive = d.MCPDestructiveHint()
	}
	if p, ok := t.(tool.EffectHintProvider); ok {
		h := p.EffectHint(args)
		hint.Known = h.Known
		hint.ReadOnly = hint.ReadOnly || h.ReadOnly
		hint.Destructive = hint.Destructive || h.Destructive
		hint.Privileged = h.Privileged
		hint.UsesNetwork = h.UsesNetwork
		hint.ExecutesCode = h.ExecutesCode
		hint.Targets = append([]string(nil), h.Targets...)
	}
	return hint
}

func (a *Agent) requiresIndependentReview() bool {
	if a == nil || a.turn.engine == nil {
		return false
	}
	for _, o := range a.turn.engine.Snapshot().Unsatisfied() {
		if o.Kind == taskcontract.ObligationIndependentReview || o.Kind == taskcontract.ObligationSecurityReview {
			return true
		}
	}
	return false
}

func (a *Agent) requiresSecurityReview() bool {
	if a == nil || a.turn.engine == nil {
		return false
	}
	for _, o := range a.turn.engine.Snapshot().Unsatisfied() {
		if o.Kind == taskcontract.ObligationSecurityReview {
			return true
		}
	}
	return false
}

func (a *Agent) hasInteractiveAsk() bool {
	if a == nil || a.svc.gate == nil {
		return false
	}
	_, ok := a.svc.gate.(interface {
		Ask(any) (bool, error)
	})
	return ok
}
