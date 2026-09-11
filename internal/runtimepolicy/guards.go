package runtimepolicy

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

// PlanGuard hard-blocks writes while Plan mode is active, including YOLO.
type PlanGuard struct{}

func (PlanGuard) BeforeTool(ctx CallContext) GuardDecision {
	if !ctx.PlanReadOnly || !ctx.Profile.MutatesState() {
		return GuardDecision{Action: GuardAbstain}
	}
	return GuardDecision{
		Action:  GuardDeny,
		Reasons: []taskcontract.ReasonCode{taskcontract.ReasonPlanBoundary},
		Message: "blocked: plan mode forbids workspace mutations until the plan is approved",
	}
}
func (PlanGuard) AfterTool(ResultContext) []evidence.Receipt { return nil }
func (PlanGuard) BeforeStop(StopContext) StopDecision        { return StopDecision{} }

// ConstraintGuard applies explicit user/host limits only.
type ConstraintGuard struct{ Constraints Constraints }

func (g ConstraintGuard) BeforeTool(ctx CallContext) GuardDecision {
	c := g.Constraints
	if ctx.Profile.MutatesState() && !c.AllowsMutation() {
		return GuardDecision{
			Action:  GuardDeny,
			Reasons: []taskcontract.ReasonCode{taskcontract.ReasonUserConstraint},
			Message: "blocked: the current constraints forbid state mutation",
		}
	}
	if (ctx.Profile.ExternalState || looksExternalCommand(ctx)) && !c.AllowsExternal() {
		return GuardDecision{
			Action:  GuardDeny,
			Reasons: []taskcontract.ReasonCode{taskcontract.ReasonUserConstraint},
			Message: "blocked: the current constraints forbid push/publish/deploy-style actions",
		}
	}
	if ctx.Verification && !c.AllowsTests() {
		return GuardDecision{
			Action:  GuardDeny,
			Reasons: []taskcontract.ReasonCode{taskcontract.ReasonUserConstraint},
			Message: "blocked: the current constraints forbid verification commands",
		}
	}
	if ctx.Verification && !c.AllowsCommand(bashCommand(ctx)) {
		return GuardDecision{
			Action:  GuardDeny,
			Reasons: []taskcontract.ReasonCode{taskcontract.ReasonUserConstraint},
			Message: "blocked: verification command is outside the user allowlist",
		}
	}
	return GuardDecision{Action: GuardAbstain}
}
func (ConstraintGuard) AfterTool(ResultContext) []evidence.Receipt { return nil }
func (ConstraintGuard) BeforeStop(StopContext) StopDecision        { return StopDecision{} }

func bashCommand(ctx CallContext) string {
	name := strings.ToLower(strings.TrimSpace(ctx.ToolName))
	if name != "bash" && name != "shell" {
		return ""
	}
	var payload struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(ctx.Args, &payload) == nil {
		return strings.TrimSpace(payload.Command)
	}
	return ""
}

func looksExternalCommand(ctx CallContext) bool {
	name := strings.ToLower(strings.TrimSpace(ctx.ToolName))
	if strings.Contains(name, "deploy") || strings.Contains(name, "publish") || strings.Contains(name, "push") {
		return true
	}
	cmd := strings.ToLower(bashCommand(ctx))
	if cmd == "" {
		return false
	}
	for _, needle := range []string{"git push", "publish", "kubectl", "deploy", "helm push"} {
		if strings.Contains(cmd, needle) {
			return true
		}
	}
	return false
}

// ContractPreconditionGuard requires todo/criteria before mapped writers.
type ContractPreconditionGuard struct {
	HasTodo     bool
	HasCriteria bool
}

func (g ContractPreconditionGuard) BeforeTool(ctx CallContext) GuardDecision {
	if !ctx.Profile.MutatesState() || ctx.Profile.ReadOnly || ctx.Verification {
		return GuardDecision{Action: GuardAbstain}
	}
	mapping := taskcontract.MapWriter(ctx.Profile, 0, ctx.WorkspaceRoot, ctx.TestsForbidden)
	mapping.Preconditions = cumulativeWritePreconditions(ctx, mapping)
	if len(mapping.Preconditions) == 0 {
		return GuardDecision{Action: GuardAbstain}
	}
	var missing []taskcontract.Obligation
	for _, o := range mapping.Preconditions {
		switch o.Kind {
		case taskcontract.ObligationTodo:
			if !g.HasTodo && !ctx.HasTodo {
				missing = append(missing, o)
			}
		case taskcontract.ObligationCriteria:
			if !g.HasCriteria && !ctx.HasCriteria {
				missing = append(missing, o)
			}
		}
	}
	if len(missing) == 0 {
		return GuardDecision{Action: GuardAllow, Preconditions: mapping.Preconditions}
	}
	return GuardDecision{
		Action:        GuardDeny,
		Preconditions: missing,
		Reasons:       []taskcontract.ReasonCode{taskcontract.ReasonFirstWriter},
		Message:       "blocked: establish a concrete todo and acceptance criteria before this class of write",
	}
}
func (ContractPreconditionGuard) AfterTool(ResultContext) []evidence.Receipt { return nil }
func (ContractPreconditionGuard) BeforeStop(StopContext) StopDecision        { return StopDecision{} }

func cumulativeWritePreconditions(ctx CallContext, mapping taskcontract.Mapping) []taskcontract.Obligation {
	if len(mapping.Preconditions) > 0 {
		return mapping.Preconditions
	}
	current := workspaceTargetKeys(ctx.Profile.TargetKeys())
	if ctx.Profile.Known && ctx.Profile.WorkspaceWrite && len(current) == 0 && len(ctx.PriorWriteTargets) > 0 {
		return multiFilePreconditions(nil)
	}
	all := append([]evidence.TargetKey(nil), ctx.PriorWriteTargets...)
	for _, target := range current {
		if !slices.Contains(all, target) {
			all = append(all, target)
		}
	}
	if len(all) < 2 || !ctx.PriorProductionWrite && !mappingHasProductionWrite(mapping) {
		return nil
	}
	return multiFilePreconditions(all)
}

func workspaceTargetKeys(targets []evidence.TargetKey) []evidence.TargetKey {
	var out []evidence.TargetKey
	for _, target := range targets {
		key := string(target)
		if strings.HasPrefix(key, "file:") || strings.HasPrefix(key, "dir:") {
			out = append(out, target)
		}
	}
	return out
}

func mappingHasProductionWrite(mapping taskcontract.Mapping) bool {
	for _, obligation := range mapping.PostSuccess {
		if obligation.Origin != taskcontract.ReasonDocsEdit {
			return true
		}
	}
	return false
}

func multiFilePreconditions(targets []evidence.TargetKey) []taskcontract.Obligation {
	return []taskcontract.Obligation{
		{Kind: taskcontract.ObligationTodo, Enforcement: taskcontract.EnforcementRecoverable, Origin: taskcontract.ReasonMultiFile, Targets: append([]evidence.TargetKey(nil), targets...)},
		{Kind: taskcontract.ObligationCriteria, Enforcement: taskcontract.EnforcementRecoverable, Origin: taskcontract.ReasonMultiFile, Targets: append([]evidence.TargetKey(nil), targets...)},
	}
}

// MutationDependencyGuard blocks later mutations after an earlier batch failure.
type MutationDependencyGuard struct{ Blocked bool }

func (g MutationDependencyGuard) BeforeTool(ctx CallContext) GuardDecision {
	if !g.Blocked || (!ctx.Profile.MutatesState() && !ctx.Verification) {
		return GuardDecision{Action: GuardAbstain}
	}
	return GuardDecision{
		Action:  GuardDeny,
		Reasons: []taskcontract.ReasonCode{taskcontract.ReasonReceipt},
		Message: "blocked: an earlier mutation in this batch failed; later mutations and verifications cannot run",
	}
}
func (MutationDependencyGuard) AfterTool(ResultContext) []evidence.Receipt { return nil }
func (MutationDependencyGuard) BeforeStop(StopContext) StopDecision        { return StopDecision{} }

// OpaqueWriterGuard asks when an unknown writer can be reviewed, else denies.
type OpaqueWriterGuard struct{}

func (OpaqueWriterGuard) BeforeTool(ctx CallContext) GuardDecision {
	name := strings.ToLower(strings.TrimSpace(ctx.ToolName))
	if !ctx.Profile.OpaqueWriter() || name == "bash" || name == "shell" {
		return GuardDecision{Action: GuardAbstain}
	}
	if ctx.Interactive {
		return GuardDecision{
			Action:  GuardAsk,
			Reasons: []taskcontract.ReasonCode{taskcontract.ReasonOpaqueWriter},
			Message: "unknown writer requires explicit approval",
		}
	}
	return GuardDecision{
		Action:  GuardDeny,
		Reasons: []taskcontract.ReasonCode{taskcontract.ReasonOpaqueWriter},
		Message: "blocked: unknown writer cannot run without an interactive approval channel",
	}
}
func (OpaqueWriterGuard) AfterTool(ResultContext) []evidence.Receipt { return nil }
func (OpaqueWriterGuard) BeforeStop(StopContext) StopDecision        { return StopDecision{} }
