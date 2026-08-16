package runtimepolicy

import (
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

// GuardAction is one monotonic preflight verdict.
type GuardAction uint8

const (
	GuardAbstain GuardAction = iota
	GuardAllow
	GuardAsk
	GuardDeny
)

// GuardDecision is one guard's immutable snapshot.
type GuardDecision struct {
	Action        GuardAction
	Preconditions []taskcontract.Obligation
	Reasons       []taskcontract.ReasonCode
	Message       string
}

// CallContext is the resolved, already-identified tool call.
type CallContext struct {
	ToolName             string
	Args                 json.RawMessage
	Profile              evidence.EffectProfile
	PlanReadOnly         bool
	Interactive          bool
	HasTodo              bool
	HasCriteria          bool
	Verification         bool
	TestsForbidden       bool
	WorkspaceRoot        string
	PriorWriteTargets    []evidence.TargetKey
	PriorProductionWrite bool
}

// ResultContext is the frozen post-execute receipt.
type ResultContext struct {
	Seq            int
	Receipt        evidence.Receipt
	Profile        evidence.EffectProfile
	WorkspaceRoot  string
	TestsForbidden bool
}

// StopContext is the host-visible state at a proposed stop.
type StopContext struct {
	GoalActive     bool
	ApprovedPlan   bool
	Opts           taskcontract.StopOptions
	IncompleteTodo bool
}

// StopDecision is the composed stop stance plus any user-visible note.
type StopDecision struct {
	Disposition taskcontract.StopDisposition
	Message     string
	Advisory    []taskcontract.Obligation
}

// Guard is one monotonic pipeline stage.
type Guard interface {
	BeforeTool(CallContext) GuardDecision
	AfterTool(ResultContext) []evidence.Receipt
	BeforeStop(StopContext) StopDecision
}

// MergeDecisions applies Deny > Ask > Allow > Abstain and concatenates
// obligations. Later guards cannot revoke a stronger action.
func MergeDecisions(decisions ...GuardDecision) GuardDecision {
	out := GuardDecision{Action: GuardAbstain}
	for _, d := range decisions {
		if d.Action > out.Action {
			out.Action = d.Action
			if d.Message != "" {
				out.Message = d.Message
			}
		} else if out.Message == "" && d.Message != "" && d.Action == out.Action {
			out.Message = d.Message
		}
		out.Preconditions = append(out.Preconditions, d.Preconditions...)
		out.Reasons = append(out.Reasons, d.Reasons...)
	}
	return out
}
